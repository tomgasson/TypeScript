package compiler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

// Capture emit through WriteFile without granting the compiler a writable FS.
// A future in-memory project-reference loader must preserve this boundary.
type declarationProducerReadOnlyFS struct{ vfs.FS }

func (*declarationProducerReadOnlyFS) WriteFile(tspath.RootedFilePath, string) error {
	panic("declaration capture must not write to the filesystem")
}

func (*declarationProducerReadOnlyFS) AppendFile(tspath.RootedFilePath, string) error {
	panic("declaration capture must not append to the filesystem")
}

type declarationProducerHost struct {
	CompilerHost
	producer *tsoptions.ParsedCommandLine
}

func (host *declarationProducerHost) GetResolvedProjectReference(fileName tspath.RootedFilePath, path tspath.PathKey) *tsoptions.ParsedCommandLine {
	if fileName == host.producer.ConfigFileName() {
		return host.producer
	}
	return host.CompilerHost.GetResolvedProjectReference(fileName, path)
}

func parseDeclarationProducerConfig(t *testing.T, fs vfs.FS, fileName tspath.RootedFilePath) *tsoptions.ParsedCommandLine {
	t.Helper()
	config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(fileName, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
	assert.Equal(t, len(diagnostics), 0)
	assert.Assert(t, config != nil)
	assert.Equal(t, len(config.Errors), 0)
	return config
}

func declarationProducerOptionsJSON(t *testing.T, config *tsoptions.ParsedCommandLine) string {
	t.Helper()
	text, err := json.Marshal(config.CompilerOptions())
	assert.NilError(t, err)
	return string(text)
}

// The captured result deliberately retains only output filenames and strings,
// not WriteFileData, source nodes, diagnostics, or the producer program.
func captureProducerDeclarations(program *Program, targets []*ast.SourceFile) (map[tspath.RootedFilePath]string, *EmitResult) {
	output := make(map[tspath.RootedFilePath]string)
	result := program.Emit(context.Background(), EmitOptions{
		TargetSourceFiles: targets,
		EmitOnly:          EmitOnlyDts,
		WriteFile: func(fileName tspath.RootedFilePath, text string, _ *WriteFileData) error {
			output[fileName] = text
			return nil
		},
	})
	return output, result
}

func TestDeclarationEmitReusesProducerConfig(t *testing.T) {
	t.Parallel()
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(map[string]string{
		"/consumer/tsconfig.json": `{
  "compilerOptions": {
    "noLib": true, "declaration": true, "isolatedDeclarations": false,
    "stripInternal": false, "module": "esnext", "moduleResolution": "bundler",
    "declarationDir": "./consumer-types", "paths": { "@model": ["./wrong-model"] }
  },
  "files": ["index.ts"], "references": [{ "path": "../producer" }]
}`,
		"/consumer/index.ts":       "export { read } from '../producer/src/index';",
		"/consumer/wrong-model.ts": "export interface Model { wrong: boolean; }",
		"/producer/tsconfig.json": `{
  "compilerOptions": {
    "noLib": true, "composite": true, "declaration": true, "isolatedDeclarations": true,
    "stripInternal": true, "module": "esnext", "moduleResolution": "bundler",
    "rootDir": "./src", "outDir": "./dist", "declarationDir": "./types",
    "paths": { "@model": ["./src/model"] }
  },
  "files": ["src/index.ts", "src/model.ts", "src/unused.ts", "src/ambient.d.ts"]
}`,
		"/producer/src/index.ts": `/// <reference path="./ambient.d.ts" preserve="true" />
import type { Model } from "@model";
export function read(value: Model): Model { return value; }
/** @internal */
export const hidden: number = 1;
export const visible: number = 1;
`,
		"/producer/src/model.ts":     "export interface Model { value: number; }",
		"/producer/src/unused.ts":    "export const unrelated: string = 'unrelated';",
		"/producer/src/ambient.d.ts": "interface ProducerGlobal { value: number; }",
	}, tspath.CaseSensitive)}
	producer := parseDeclarationProducerConfig(t, fs, "/producer/tsconfig.json")
	consumer := parseDeclarationProducerConfig(t, fs, "/consumer/tsconfig.json")
	producerOptions := declarationProducerOptionsJSON(t, producer)
	consumerOptions := declarationProducerOptionsJSON(t, consumer)
	host := &declarationProducerHost{
		CompilerHost: NewCompilerHost(fs, "/", nil, nil, nil),
		producer:     producer,
	}
	consumerProgram := NewProgram(ProgramOptions{
		Config: consumer, Host: host, SingleThreaded: core.TSTrue, UseSourceOfProjectReference: true,
	})
	consumerFile := consumerProgram.GetSourceFile("/producer/src/index.ts")
	assert.Assert(t, consumerFile != nil)
	reference := consumerProgram.GetProjectReferenceFromSource(consumerFile.PathKey())
	assert.Assert(t, reference != nil)
	assert.Equal(t, reference.Resolved, producer)
	assert.Equal(t, reference.Source, tspath.RootedFilePath("/producer/src/index.ts"))
	assert.Equal(t, reference.OutputDts, tspath.RootedFilePath("/producer/types/index.d.ts"))
	assert.Equal(t, consumerProgram.Options().IsolatedDeclarations, core.TSFalse)
	assert.Equal(t, reference.Resolved.CompilerOptions().IsolatedDeclarations, core.TSTrue)

	// Reuse the complete producer configuration. A selected emit changes neither
	// its root list nor the config metadata used for output paths and diagnostics.
	producerProgram := NewProgram(ProgramOptions{
		Config: reference.Resolved, Host: host, SingleThreaded: core.TSTrue,
	})
	assert.Equal(t, producerProgram.opts.Config, producer)
	file := producerProgram.GetSourceFile(reference.Source)
	assert.Assert(t, file != nil)
	assert.Assert(t, file != consumerFile, "producer and consumer programs own distinct source trees")
	assert.Equal(t, file.FileName(), reference.Source)
	assert.Equal(t, producerProgram.GetSourceFile("/producer/src/unused.ts").FileName(), tspath.RootedFilePath("/producer/src/unused.ts"))
	resolved := producerProgram.GetResolvedModuleFromModuleSpecifier(file, file.Imports()[0])
	assert.Assert(t, resolved.IsResolved())
	assert.Equal(t, resolved.ResolvedFileName, tspath.RootedFilePath("/producer/src/model.ts"))

	selected, result := captureProducerDeclarations(producerProgram, []*ast.SourceFile{file})
	assert.Equal(t, len(result.Diagnostics), 0)
	assert.Equal(t, result.EmitSkipped, false)
	assert.Equal(t, len(selected), 1)
	text, ok := selected[reference.OutputDts]
	assert.Assert(t, ok, "selected output must use the producer's declarationDir")
	assert.Assert(t, strings.Contains(text, `from "@model"`), "declaration imports preserve their original specifier")
	assert.Assert(t, strings.Contains(text, "export declare function read(value: Model): Model;"))
	assert.Assert(t, strings.Contains(text, "export declare const visible: number;"))
	assert.Assert(t, !strings.Contains(text, "hidden"), "producer stripInternal controls declaration output")
	declaration := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: reference.OutputDts, PathKey: reference.OutputDtsPath,
	}, text, core.ScriptKindTS)
	assert.Equal(t, len(declaration.Diagnostics()), 0)
	assert.Equal(t, declaration.IsDeclarationFile, true)
	assert.Equal(t, declaration.FileName(), reference.OutputDts)
	assert.Equal(t, len(declaration.ReferencedFiles), 1)
	assert.Equal(t, declaration.ReferencedFiles[0].Preserve, true)
	assert.Equal(t, reference.OutputDts.Directory().ResolveFile(declaration.ReferencedFiles[0].FileName), tspath.RootedFilePath("/producer/src/ambient.d.ts"))

	// Subsequent multi-file printing can reuse writers. Previously returned text
	// must remain valid, and selected emit must agree with ordinary producer emit.
	all, fullResult := captureProducerDeclarations(producerProgram, nil)
	assert.Equal(t, len(fullResult.Diagnostics), 0)
	assert.Equal(t, fullResult.EmitSkipped, false)
	assert.Equal(t, len(all), 3)
	assert.Equal(t, all[reference.OutputDts], text)
	assert.Equal(t, selected[reference.OutputDts], text)
	assert.Assert(t, !fs.FileExists(reference.OutputDts))
	assert.Assert(t, !fs.DirectoryExists("/producer/types"))
	assert.Assert(t, !fs.DirectoryExists("/consumer/consumer-types"))
	assert.Equal(t, declarationProducerOptionsJSON(t, producer), producerOptions)
	assert.Equal(t, declarationProducerOptionsJSON(t, consumer), consumerOptions)
}

func TestDeclarationEmitProducerModuleExtensions(t *testing.T) {
	t.Parallel()
	for _, extension := range []string{"mts", "cts"} {
		t.Run(extension, func(t *testing.T) {
			fileName := tspath.RootedFilePathFromNormalized("/producer/src/index." + extension)
			expected := tspath.RootedFilePathFromNormalized("/producer/types/index.d." + extension)
			fs := &declarationProducerReadOnlyFS{vfstest.FromMap(map[string]string{
				"/producer/tsconfig.json": `{"compilerOptions":{"noLib":true,"composite":true,"declaration":true,"isolatedDeclarations":true,"module":"nodenext","rootDir":"./src","declarationDir":"./types"},"files":["src/index.` + extension + `"]}`,
				fileName.AsString():       "export const value: number = 1;",
			}, tspath.CaseSensitive)}
			config := parseDeclarationProducerConfig(t, fs, "/producer/tsconfig.json")
			program := NewProgram(ProgramOptions{Config: config, Host: NewCompilerHost(fs, "/", nil, nil, nil), SingleThreaded: core.TSTrue})
			file := program.GetSourceFile(fileName)
			assert.Assert(t, file != nil)
			output, result := captureProducerDeclarations(program, []*ast.SourceFile{file})
			assert.Equal(t, len(result.Diagnostics), 0)
			assert.Equal(t, result.EmitSkipped, false)
			assert.Equal(t, len(output), 1)
			text, ok := output[expected]
			assert.Assert(t, ok)
			assert.Assert(t, strings.Contains(text, "export declare const value: number;"))
			declaration := parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: expected, PathKey: fs.CaseSensitivity().PathKey(expected.AsPath()),
			}, text, core.ScriptKindTS)
			assert.Equal(t, declaration.FileName(), expected)
			assert.Equal(t, declaration.IsDeclarationFile, true)
			assert.Assert(t, !fs.FileExists(expected))
		})
	}
}

func TestDeclarationEmitProducerRejectsInvalidIsolatedDeclaration(t *testing.T) {
	t.Parallel()
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(map[string]string{
		"/producer/tsconfig.json": `{"compilerOptions":{"noLib":true,"declaration":true,"isolatedDeclarations":true,"rootDir":"./src","declarationDir":"./types"},"files":["src/index.ts"]}`,
		"/producer/src/index.ts":  "function internal(): number { return 1; }\nexport function value() { return internal(); }",
	}, tspath.CaseSensitive)}
	config := parseDeclarationProducerConfig(t, fs, "/producer/tsconfig.json")
	program := NewProgram(ProgramOptions{Config: config, Host: NewCompilerHost(fs, "/", nil, nil, nil), SingleThreaded: core.TSTrue})
	file := program.GetSourceFile("/producer/src/index.ts")
	assert.Assert(t, file != nil)
	output, result := captureProducerDeclarations(program, []*ast.SourceFile{file})
	assert.Equal(t, result.EmitSkipped, true)
	assert.Equal(t, len(result.Diagnostics), 1)
	assert.Equal(t, result.Diagnostics[0].Code(), int32(9013))
	assert.Equal(t, result.Diagnostics[0].File(), file, "extraction diagnostics must identify the original producer source")
	assert.Equal(t, len(output), 0, "failed isolated declaration emit must not supply reusable text")
	assert.Assert(t, !fs.FileExists("/producer/types/index.d.ts"))
}
