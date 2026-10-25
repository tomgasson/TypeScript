package project

import (
	"context"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestGeneratedDeclarationInvalidation(t *testing.T) {
	const producerConfig = `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"declarationMap":true,"rootDir":".","declarationDir":"../types","module":"esnext","moduleResolution":"bundler"},"files":["index.ts"]}`
	const producerSource = "export function value(): number { return 1; }\n"
	const consumerSource = "import { value } from '../producer/index';\nexport const result: number = value();\n"
	const consumerURI lsproto.DocumentUri = "file:///consumer/index.ts"
	const output tspath.RootedFilePath = "/types/index.d.ts"
	const mapName tspath.RootedFilePath = "/types/index.d.ts.map"
	for _, c := range []struct {
		name     string
		fileName tspath.RootedFilePath
		invalid  string
		repaired string
	}{
		{"ProducerConfig", "/producer/tsconfig.json", strings.Replace(producerConfig, `"isolatedDeclarations":true`, `"isolatedDeclarations":false`, 1), producerConfig},
		{"InvalidSignature", "/producer/index.ts", "function internal(): number { return 1; }\nexport function value() { return internal(); }\n", "\n\nexport function value(): number { return 2; }\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			fs := vfstest.FromMap(map[string]string{
				"/producer/tsconfig.json": producerConfig,
				"/producer/index.ts":      producerSource,
				"/consumer/tsconfig.json": `{"compilerOptions":{"noLib":true,"module":"esnext","moduleResolution":"bundler","disableSourceOfProjectReferenceRedirect":true},"files":["index.ts"],"references":[{"path":"../producer"}]}`,
				"/consumer/index.ts":      consumerSource,
			}, tspath.CaseSensitive)
			session := NewSession(&SessionInit{
				BackgroundCtx: ctx, FS: fs, Client: noopClient{},
				Options: &SessionOptions{CurrentDirectory: "/", DefaultLibraryPath: "/", PositionEncoding: lsproto.PositionEncodingKindUTF8},
			})
			t.Cleanup(session.Close)
			preferences := session.Config()
			preferences.IncludeCompletionsForModuleExports = core.TSFalse
			session.InitializeWithUserConfig(preferences)
			session.DidOpenFile(ctx, consumerURI, 1, consumerSource, lsproto.LanguageKindTypeScript)

			check := func(generated bool, definitionLine uint32) ParseCacheKey {
				t.Helper()
				service, err := session.GetLanguageService(ctx, consumerURI)
				assert.NilError(t, err)
				session.WaitForBackgroundTasks()
				program := service.GetProgram()
				diagnostics := program.GetSemanticDiagnostics(ctx, program.GetSourceFile("/consumer/index.ts"))
				if generated {
					assert.Equal(t, len(diagnostics), 0, "%v", diagnostics)
				} else {
					assert.Equal(t, len(diagnostics), 1, "%v", diagnostics)
					assert.Equal(t, diagnostics[0].Code(), int32(6305))
				}
				for _, name := range []tspath.RootedFilePath{output, mapName} {
					_, ok := program.GetGeneratedDeclarationText(tspath.PathKey(name))
					assert.Equal(t, ok, generated, "%s", name)
					_, ok = service.ReadFile(name)
					assert.Equal(t, ok, generated, "%s", name)
					assert.Equal(t, session.Snapshot().GetFile(name) != nil, generated, "%s", name)
					assert.Assert(t, !fs.FileExists(name), "generation must not create a physical output: %s", name)
				}
				assert.Assert(t, program.GetSourceFile("/producer/index.ts") == nil)
				for key := range session.parseCache.entries.Keys() {
					assert.Assert(t, key.FileName != "/producer/index.ts", "producer implementation entered the editor parse cache")
				}
				file := program.GetSourceFile(output)
				assert.Equal(t, file != nil, generated)
				if !generated {
					return ParseCacheKey{}
				}
				key := parseCacheKeyForFile(file)
				assert.Assert(t, session.parseCache.Has(key))
				definition, err := service.ProvideDefinition(ctx, consumerURI, lsproto.Position{Line: 0, Character: 10})
				assert.NilError(t, err)
				assert.Assert(t, definition.Locations != nil)
				assert.Equal(t, len(*definition.Locations), 1)
				assert.Equal(t, (*definition.Locations)[0].Uri, lsproto.DocumentUri("file:///producer/index.ts"))
				assert.Equal(t, (*definition.Locations)[0].Range.Start.Line, definitionLine)
				return key
			}
			change := func(text string) {
				t.Helper()
				assert.NilError(t, fs.WriteFile(c.fileName, text))
				session.DidChangeWatchedFiles(ctx, []*lsproto.FileEvent{{Uri: lsproto.DocumentUri("file://" + c.fileName.AsString()), Type: lsproto.FileChangeTypeChanged}})
			}
			oldKey := check(true, 0)
			change(c.invalid)
			check(false, 0)
			assert.Assert(t, !session.parseCache.Has(oldKey), "invalidated declaration remains owned by the editor parse cache")
			change(c.repaired)
			check(true, core.IfElse(c.name == "InvalidSignature", uint32(2), uint32(0)))
		})
	}
}
