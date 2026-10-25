package project

import (
	"context"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestClonedProgramRetainsMissingResolutionDirectories(t *testing.T) {
	t.Parallel()
	for _, createdURI := range []lsproto.DocumentUri{"file:///deps/pkg/index.d.ts", "file:///deps/pkg"} {
		t.Run(string(createdURI), func(t *testing.T) {
			t.Parallel()
			const uri lsproto.DocumentUri = "file:///consumer/index.ts"
			const source = "import { value } from '../deps/pkg';\nexport const result: number = value;\nexport const edit: number = 0;\n"
			fs := vfstest.FromMap(map[string]string{
				"/consumer/tsconfig.json": `{"compilerOptions":{"noLib":true,"strict":true,"module":"esnext","moduleResolution":"bundler"},"files":["index.ts"]}`,
				"/consumer/index.ts":      source,
			}, tspath.CaseSensitive)
			session := NewSession(&SessionInit{
				BackgroundCtx: context.Background(), FS: fs, Client: noopClient{},
				Options: &SessionOptions{CurrentDirectory: "/", DefaultLibraryPath: "/", PositionEncoding: lsproto.PositionEncodingKindUTF8},
			})
			defer session.Close()
			preferences := session.Config()
			preferences.IncludeCompletionsForModuleExports = core.TSFalse
			session.InitializeWithUserConfig(preferences)
			ctx := core.WithCheckerLifetime(context.Background(), core.CheckerLifetimeDiagnostics)
			check := func(unresolved bool) *Project {
				t.Helper()
				service, err := session.GetLanguageService(ctx, uri)
				assert.NilError(t, err)
				program := service.GetProgram()
				diagnostics := program.GetSemanticDiagnostics(ctx, program.GetSourceFile("/consumer/index.ts"))
				if unresolved {
					assert.Equal(t, len(diagnostics), 1)
					assert.Equal(t, diagnostics[0].Code(), int32(2307))
				} else {
					assert.Equal(t, len(diagnostics), 0)
					assert.Assert(t, program.GetSourceFile("/deps/pkg/index.d.ts") != nil)
				}
				session.WaitForBackgroundTasks()
				return session.Snapshot().GetDefaultProject(uri)
			}
			session.DidOpenFile(ctx, uri, 1, source, lsproto.LanguageKindTypeScript)
			before := check(true)
			createdPath := createdURI.PathKey(tspath.CaseSensitive)
			assert.Assert(t, !before.host.sourceFS.SeenFile(createdPath), "creation must be detected through its missing parent directory")
			assert.Assert(t, before.host.sourceFS.SeenFileOrMissingParentDirectory(createdPath))

			// Preserve imports and graph shape so this edit takes the clone path.
			session.DidChangeFile(ctx, uri, 2, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{
				Partial: &lsproto.TextDocumentContentChangePartial{
					Range: lsproto.Range{Start: lsproto.Position{Line: 2, Character: 28}, End: lsproto.Position{Line: 2, Character: 29}},
					Text:  "1",
				},
			}})
			cloned := check(true)
			assert.Equal(t, cloned.ProgramUpdateKind, ProgramUpdateKindCloned)

			// This package is outside the consumer's explicit root list. Config
			// root-file discovery must not accidentally repair lost resolution state.
			assert.NilError(t, fs.WriteFile("/deps/pkg/index.d.ts", "export declare const value: number;"))
			session.DidChangeWatchedFiles(ctx, []*lsproto.FileEvent{{Uri: createdURI, Type: lsproto.FileChangeTypeCreated}})
			after := check(false)
			assert.Assert(t, after.Program != cloned.Program)
		})
	}
}
