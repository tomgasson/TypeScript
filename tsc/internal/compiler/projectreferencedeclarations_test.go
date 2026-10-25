package compiler

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func projectReferenceDeclarationFixture(t *testing.T, producerSource, consumerSource string, producerOptions, consumerOptions map[string]any) map[string]string {
	t.Helper()
	producer := map[string]any{
		"noLib": true, "strict": true, "composite": true, "isolatedDeclarations": true,
		"module": "esnext", "moduleResolution": "bundler", "outDir": "./dist",
	}
	consumer := map[string]any{"noLib": true, "strict": true, "module": "esnext", "moduleResolution": "bundler"}
	maps.Copy(producer, producerOptions)
	maps.Copy(consumer, consumerOptions)
	configText := func(config map[string]any) string {
		text, err := json.Marshal(config)
		assert.NilError(t, err)
		return string(text)
	}
	return map[string]string{
		"/globals.d.ts": `interface Object {} interface Function {} interface CallableFunction {} interface NewableFunction {} interface IArguments {} interface String {} interface Number {} interface Boolean {} interface RegExp {} interface Array<T> { length: number; [n: number]: T; }`,
		"/producer/tsconfig.json": configText(map[string]any{
			"compilerOptions": producer, "files": []string{"../globals.d.ts"}, "include": []string{"*.ts"},
		}),
		"/consumer/tsconfig.json": configText(map[string]any{
			"compilerOptions": consumer, "files": []string{"../globals.d.ts", "index.ts"},
			"references": []map[string]string{{"path": "../producer"}},
		}),
		"/producer/index.ts": producerSource,
		"/consumer/index.ts": consumerSource,
	}
}

func newProjectReferenceDeclarationProgram(t *testing.T, fs vfs.FS, configName tspath.RootedFilePath, options ProgramOptions) *Program {
	t.Helper()
	options.Config = parseDeclarationProducerConfig(t, fs, configName)
	options.Host = NewCompilerHost(fs, "/", nil, nil, nil)
	options.SingleThreaded = core.TSTrue
	return NewProgram(options)
}

func projectReferenceDeclarationDiagnostics(program *Program) []*ast.Diagnostic {
	ctx := context.Background()
	result := slices.Clone(program.GetProgramDiagnostics())
	result = append(result, program.GetSyntacticDiagnostics(ctx, nil)...)
	result = append(result, program.GetGlobalDiagnostics(ctx)...)
	result = append(result, program.GetSemanticDiagnostics(ctx, nil)...)
	slices.SortFunc(result, ast.CompareDiagnostics)
	return result
}

func TestProjectReferenceDeclarationsMatchDisk(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name            string
		source          string
		consumer        string
		producerOptions map[string]any
		consumerOptions map[string]any
		extraFiles      map[string]string
		expectedCodes   []int32
		declarationText string
	}{
		{
			name:     "implementation-only error without noEmitOnError",
			source:   `export function value(): number { return "wrong"; }`,
			consumer: `import { value } from '../producer/index'; const result: number = value();`,
		},
		{
			name:            "declaration map supersedes stale disk map",
			source:          `export const value: number = 1;`,
			consumer:        `import { value } from '../producer/index'; const result: number = value;`,
			producerOptions: map[string]any{"declarationMap": true},
			extraFiles:      map[string]string{"/producer/dist/index.d.ts.map": "stale disk map"},
		},
		{
			name:   "private generic field",
			source: `export class Token<T> { private value!: T; }`,
			consumer: `import { Token } from '../producer/index';
declare let numberToken: Token<number>;
declare let stringToken: Token<string>;
numberToken = stringToken;`,
			// Ordinary declarations omit the private property's type. Checking the
			// implementation instead would incorrectly distinguish these instances.
			declarationText: "private value;",
		},
		{
			name: "strip internal",
			source: `/** @internal */
export const hidden: number = 1;
export const visible: number = 2;`,
			consumer:        `import { hidden, visible } from '../producer/index'; const value: number = visible;`,
			producerOptions: map[string]any{"stripInternal": true},
			expectedCodes:   []int32{2305},
		},
		{
			name:            "preserved const enum",
			source:          `export const enum Value { One = 1 }`,
			consumer:        `import { Value } from '../producer/index'; const value: number = Value.One;`,
			producerOptions: map[string]any{"preserveConstEnums": true},
			consumerOptions: map[string]any{"isolatedModules": true, "verbatimModuleSyntax": true},
		},
		{
			name:            "erased const enum",
			source:          `export const enum Value { One = 1 }`,
			consumer:        `import { Value } from '../producer/index'; const value: number = Value.One;`,
			producerOptions: map[string]any{"preserveConstEnums": false},
			consumerOptions: map[string]any{"isolatedModules": true},
			expectedCodes:   []int32{2748},
		},
		{
			name: "module and global augmentations",
			source: `import './augment';
export type { Model } from './model';
declare global { interface ProducerGlobal { present: number; } }`,
			consumer: `import type { Model } from '../producer/index';
const value: Model = { original: 1, added: 'yes' };
const global: ProducerGlobal = { present: value.original };`,
			extraFiles: map[string]string{
				"/producer/model.ts": `export interface Model { original: number; }`,
				"/producer/augment.ts": `import './model';
declare module './model' { interface Model { added: string; } }`,
			},
			declarationText: `import './augment';`,
		},
		{
			name:     "source import cycle",
			source:   `import type { Other } from './other'; export interface Model { other?: Other; }`,
			consumer: `import type { Model } from '../producer/index'; const model: Model = {};`,
			extraFiles: map[string]string{
				"/producer/other.ts": `import type { Model } from './index'; export interface Other { model?: Model; }`,
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := projectReferenceDeclarationFixture(t, c.source, c.consumer, c.producerOptions, c.consumerOptions)
			maps.Copy(files, c.extraFiles)
			// The reference is the ordinary producer emitter writing real VFS
			// outputs, with exactly the same configuration and source files.
			diskFS := vfstest.FromMap(files, tspath.CaseSensitive)
			producer := newProjectReferenceDeclarationProgram(t, diskFS, "/producer/tsconfig.json", ProgramOptions{})
			output, emitted := captureProducerDeclarations(producer, nil)
			assert.Assert(t, emitted != nil)
			assert.Equal(t, emitted.EmitSkipped, false)
			assert.Equal(t, len(emitted.Diagnostics), 0, "%v", emitted.Diagnostics)
			for path, text := range output {
				assert.NilError(t, diskFS.WriteFile(path, text))
			}
			disk := newProjectReferenceDeclarationProgram(t, diskFS, "/consumer/tsconfig.json", ProgramOptions{})
			diskDiagnostics := projectReferenceDeclarationDiagnostics(disk)
			codes := core.Map(diskDiagnostics, func(d *ast.Diagnostic) int32 { return d.Code() })
			slices.Sort(codes)
			assert.Assert(t, slices.Equal(codes, c.expectedCodes), "unexpected disk diagnostics: %v", diskDiagnostics)
			memoryFS := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
			generated := newProjectReferenceDeclarationProgram(t, memoryFS, "/consumer/tsconfig.json", ProgramOptions{})
			for _, program := range []*Program{disk, generated} {
				declaration := program.GetSourceFile("/producer/dist/index.d.ts")
				assert.Assert(t, declaration != nil)
				assert.Equal(t, declaration.IsDeclarationFile, true)
				assert.Equal(t, declaration.Text(), output["/producer/dist/index.d.ts"])
				assert.Assert(t, program.GetSourceFile("/producer/index.ts") == nil)
				if c.declarationText != "" {
					assert.Assert(t, strings.Contains(declaration.Text(), c.declarationText), "%s", declaration.Text())
				}
			}
			generatedDiagnostics := projectReferenceDeclarationDiagnostics(generated)
			assert.Assert(t, slices.EqualFunc(diskDiagnostics, generatedDiagnostics, ast.EqualDiagnostics), "disk: %v; generated: %v", diskDiagnostics, generatedDiagnostics)
			resolutionHost := generated.projectReferenceFileMapper.resolutionHost(&compilerResolutionHost{host: generated.Host(), baseDirectory: generated.BaseDirectory()}, nil)
			for path, text := range output {
				if path.ExtensionIs(".map") {
					actual, ok := resolutionHost.FS().ReadFile(path)
					assert.Assert(t, ok)
					assert.Equal(t, actual, text, "generated declarations must use their matching maps")
				}
			}
			assert.Assert(t, !memoryFS.FileExists("/producer/dist/index.d.ts"))
			if len(c.extraFiles) == 0 {
				assert.Assert(t, !memoryFS.DirectoryExists("/producer/dist"))
			}
		})
	}
}

func TestProjectReferenceDeclarationsEligibility(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name            string
		producerOptions map[string]any
		consumerOptions map[string]any
		programOptions  ProgramOptions
		generated       bool
		source          bool
	}{
		{name: "producer enables isolated declarations", generated: true},
		{name: "consumer setting does not qualify producer", producerOptions: map[string]any{"isolatedDeclarations": false}, consumerOptions: map[string]any{"isolatedDeclarations": true, "declaration": true}},
		{name: "source redirect remains source", programOptions: ProgramOptions{UseSourceOfProjectReference: true}, source: true},
		{name: "disabled source redirect uses declarations", consumerOptions: map[string]any{"disableSourceOfProjectReferenceRedirect": true}, programOptions: ProgramOptions{UseSourceOfProjectReference: true}, generated: true},
		{name: "custom resolver retains existing behavior", programOptions: ProgramOptions{CreateModuleResolver: func(options module.ResolverOptions) module.Resolver { return module.NewResolver(options) }}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := projectReferenceDeclarationFixture(t, "export const value: number = 1;", "import { value } from '../producer/index'; const result: number = value;", c.producerOptions, c.consumerOptions)
			fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
			program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", c.programOptions)
			assert.Equal(t, program.GetSourceFile("/producer/dist/index.d.ts") != nil, c.generated)
			assert.Equal(t, program.GetSourceFile("/producer/index.ts") != nil, c.source)
			diagnostics := projectReferenceDeclarationDiagnostics(program)
			if c.generated || c.source {
				assert.Equal(t, len(diagnostics), 0, "%v", diagnostics)
			} else {
				assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
			}
			assert.Assert(t, !fs.FileExists("/producer/dist/index.d.ts"))
		})
	}
}

func TestProjectReferenceDeclarationsDiskPrecedence(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t, "export const value: number = 1;", "import { value } from '../producer/index'; const result: string = value;", nil, nil)
	const declaration = "export declare const value: string;"
	files["/producer/dist/index.d.ts"] = declaration
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
	program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
	file := program.GetSourceFile("/producer/dist/index.d.ts")
	assert.Assert(t, file != nil)
	assert.Equal(t, file.Text(), declaration)
	assert.Equal(t, len(projectReferenceDeclarationDiagnostics(program)), 0)
}

type projectReferenceDeclarationReadFS struct {
	vfs.FS
	unreadable tspath.RootedFilePath
	mu         sync.Mutex
	reads      map[tspath.RootedFilePath]int
}

func (fs *projectReferenceDeclarationReadFS) ReadFile(path tspath.RootedFilePath) (string, bool) {
	fs.mu.Lock()
	fs.reads[path]++
	fs.mu.Unlock()
	if path == fs.unreadable {
		return "", false
	}
	return fs.FS.ReadFile(path)
}

func TestProjectReferenceDeclarationsUnreadableDiskOutputWins(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t, "export const value: number = 1;", "import { value } from '../producer/index'; value;", nil, nil)
	files["/producer/dist/index.d.ts"] = "export declare const value: number;"
	fs := &projectReferenceDeclarationReadFS{
		FS:         &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)},
		unreadable: "/producer/dist/index.d.ts",
		reads:      make(map[tspath.RootedFilePath]int),
	}
	program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
	assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") == nil)
	assert.Equal(t, fs.reads["/producer/index.ts"], 0, "an existing unreadable declaration must not trigger extraction")
	diagnostics := projectReferenceDeclarationDiagnostics(program)
	assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
}

func TestProjectReferenceDeclarationsParallelOutputsShareProducer(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t, "export const value: number = 1;", `import { value } from '../producer/index';
import { left } from '../producer/left';
import { right } from '../producer/right';
const result: number = value + left + right;`, nil, nil)
	files["/producer/left.ts"] = "export const left: number = 2;"
	files["/producer/right.ts"] = "export const right: number = 3;"
	fs := &projectReferenceDeclarationReadFS{
		FS:    &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)},
		reads: make(map[tspath.RootedFilePath]int),
	}
	program := NewProgram(ProgramOptions{
		Config:         parseDeclarationProducerConfig(t, fs, "/consumer/tsconfig.json"),
		Host:           NewCompilerHost(fs, "/", nil, nil, nil),
		SingleThreaded: core.TSFalse,
	})
	for _, name := range []string{"index", "left", "right"} {
		source := tspath.RootedFilePathFromNormalized("/producer/" + name + ".ts")
		output := tspath.RootedFilePathFromNormalized("/producer/dist/" + name + ".d.ts")
		assert.Assert(t, program.GetSourceFile(output) != nil)
		assert.Equal(t, fs.reads[source], 1, "all output requests must share one producer parse")
		assert.Assert(t, program.GetSourceFile(source) == nil)
		assert.Assert(t, !fs.FileExists(output))
	}
	assert.Equal(t, len(projectReferenceDeclarationDiagnostics(program)), 0)
}

func TestProjectReferenceDeclarationsRespectOutputOwner(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t, "", "", nil, nil)
	files["/consumer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"module":"esnext","moduleResolution":"bundler"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../a"},{"path":"../b"}]}`
	files["/a/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"outDir":"../shared","module":"esnext","moduleResolution":"bundler"},"files":["../globals.d.ts","index.ts","a.ts"]}`
	files["/b/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"outDir":"../shared","module":"esnext","moduleResolution":"bundler"},"files":["../globals.d.ts","index.ts","b.ts"]}`
	files["/a/index.ts"] = "export const value: number = 1;"
	files["/a/a.ts"] = "export const a: number = 1;"
	files["/b/index.ts"] = "export const value: string = 'b';"
	files["/b/b.ts"] = "export const b: number = 1;"
	// The serial loader processes these tasks in reverse order: first B's
	// shared output, then B's unique output, then A's unique output.
	files["/consumer/index.ts"] = `import { a } from '../a/a';
import { b } from '../b/b';
import { value } from '../shared/index';
const result: string = value;`
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
	program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
	reference := program.GetProjectReferenceFromOutputDts("/shared/index.d.ts")
	assert.Assert(t, reference != nil)
	assert.Equal(t, reference.Source, tspath.RootedFilePath("/b/index.ts"))
	assert.Assert(t, program.GetSourceFile("/shared/a.d.ts") != nil)
	assert.Assert(t, program.GetSourceFile("/shared/b.d.ts") != nil)
	declaration := program.GetSourceFile("/shared/index.d.ts")
	assert.Assert(t, declaration != nil)
	assert.Assert(t, strings.Contains(declaration.Text(), "value: string"))
	text, ok := program.GetGeneratedDeclarationText("/shared/index.d.ts")
	assert.Assert(t, ok)
	assert.Equal(t, text, declaration.Text(), "another producer must not replace the mapped owner's output")
	assert.Equal(t, len(projectReferenceDeclarationDiagnostics(program)), 0)
}

func TestProjectReferenceDeclarationsFailedExplicitOutputStillReportsMissing(t *testing.T) {
	t.Parallel()
	for _, rootFile := range []bool{false, true} {
		t.Run(core.IfElse(rootFile, "root file", "triple slash"), func(t *testing.T) {
			files := projectReferenceDeclarationFixture(t,
				"function local(): number { return 1; } export function value() { return local(); }",
				"/// <reference path='../producer/dist/index.d.ts' />", nil, nil)
			if rootFile {
				files["/consumer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"module":"esnext","moduleResolution":"bundler"},"files":["../globals.d.ts","../producer/dist/index.d.ts"],"references":[{"path":"../producer"}]}`
			}
			fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
			program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
			assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") == nil)
			diagnostics := projectReferenceDeclarationDiagnostics(program)
			assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6053 }), "%v", diagnostics)
		})
	}
}

func TestProjectReferenceDeclarationsFailureKeepsMissingOutput(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		source  string
		options map[string]any
	}{
		{name: "invalid isolated declaration", source: "function local(): number { return 1; } export function value() { return local(); }"},
		{name: "syntax error", source: "export const value: number = ;"},
		{name: "invalid producer option", source: "export const value: number = 1;", options: map[string]any{"unrecognizedDeclarationOption": true}},
		{name: "incompatible isolated declaration options", source: "export const value: number = 1;", options: map[string]any{"allowJs": true}},
		{name: "no emit", source: "export const value: number = 1;", options: map[string]any{"noEmit": true}},
		{name: "no emit on error", source: "export const value: number = 'wrong';", options: map[string]any{"noEmitOnError": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := projectReferenceDeclarationFixture(t, c.source, "import { value } from '../producer/index'; value;", c.options, nil)
			fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
			program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
			assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") == nil)
			assert.Assert(t, program.GetSourceFile("/producer/index.ts") == nil)
			diagnostics := projectReferenceDeclarationDiagnostics(program)
			assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
			assert.Assert(t, !fs.FileExists("/producer/dist/index.d.ts"))
		})
	}
}

func TestProjectReferenceDeclarationsProducerUsesDependencyDeclarations(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t,
		"import { hidden } from '../dep/index'; export function value(): number { return hidden; }",
		"import { value } from '../producer/index'; const result: number = value();", nil, nil)
	files["/producer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"isolatedDeclarations":true,"noEmitOnError":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../dep"}]}`
	files["/dep/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"isolatedDeclarations":true,"stripInternal":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"]}`
	files["/dep/index.ts"] = "/** @internal */\nexport const hidden: number = 1;\nexport const visible: number = 2;"
	fs := vfstest.FromMap(files, tspath.CaseSensitive)
	dependency := newProjectReferenceDeclarationProgram(t, fs, "/dep/tsconfig.json", ProgramOptions{})
	output, result := captureProducerDeclarations(dependency, nil)
	assert.Equal(t, result.EmitSkipped, false)
	assert.Equal(t, len(result.Diagnostics), 0)
	for path, text := range output {
		assert.NilError(t, fs.WriteFile(path, text))
	}
	producer := newProjectReferenceDeclarationProgram(t, fs, "/producer/tsconfig.json", ProgramOptions{})
	output, result = captureProducerDeclarations(producer, nil)
	assert.Equal(t, result.EmitSkipped, true)
	assert.Equal(t, len(output), 0)
	assert.Assert(t, slices.ContainsFunc(result.Diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 2305 }), "%v", result.Diagnostics)
	consumer := newProjectReferenceDeclarationProgram(t, &declarationProducerReadOnlyFS{fs}, "/consumer/tsconfig.json", ProgramOptions{})
	assert.Assert(t, consumer.GetSourceFile("/producer/dist/index.d.ts") == nil, "source redirection must not expose stripped dependency exports to the producer")
	diagnostics := projectReferenceDeclarationDiagnostics(consumer)
	assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
}

func TestProjectReferenceDeclarationsCycleDoesNotReenterExtraction(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationFixture(t,
		"import type { Peer } from '../peer/index'; export interface Value { peer: Peer; }",
		"import type { Value } from '../producer/index'; declare const value: Value;", nil, nil)
	files["/producer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"noEmitOnError":true,"disableSourceOfProjectReferenceRedirect":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../peer"}]}`
	files["/peer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../producer"}]}`
	files["/peer/index.ts"] = "import type { Value } from '../producer/index'; export interface Peer { value: Value; }"
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
	program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
	assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") == nil)
	assert.Assert(t, program.GetSourceFile("/peer/index.ts") == nil)
	diagnostics := projectReferenceDeclarationDiagnostics(program)
	assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
	assert.Assert(t, !fs.FileExists("/producer/dist/index.d.ts"))
	assert.Assert(t, !fs.FileExists("/peer/dist/index.d.ts"))
}
