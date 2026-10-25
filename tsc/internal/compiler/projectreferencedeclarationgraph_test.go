package compiler

import (
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func projectReferenceDeclarationGraphFixture(t *testing.T) map[string]string {
	t.Helper()
	files := projectReferenceDeclarationFixture(t, `import type { Item } from '../dependency/index';
export type { Item } from '../dependency/index';
export { value } from '../dependency/index';
export function read(item: Item): number { return item.value; }`,
		`import { read, value } from '../producer/index';
import type { Item } from '../producer/index';
const item: Item = { value };
const wrong: string = read(item);`, nil, nil)
	files["/producer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"isolatedDeclarations":true,"noEmitOnError":true,"disableSourceOfProjectReferenceRedirect":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../dependency"}]}`
	files["/dependency/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"isolatedDeclarations":true,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"]}`
	files["/dependency/index.ts"] = "export interface Item { value: number; } export const value: number = 1;"
	return files
}

func TestProjectReferenceDeclarationGraph(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name              string
		isolated          bool
		invalidDependency bool
	}{
		{name: "dependency first", isolated: true},
		{name: "dependency does not enable isolated declarations"},
		{name: "dependency declaration fails", isolated: true, invalidDependency: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := projectReferenceDeclarationGraphFixture(t)
			if !c.isolated {
				files["/dependency/tsconfig.json"] = strings.Replace(files["/dependency/tsconfig.json"], `"isolatedDeclarations":true`, `"isolatedDeclarations":false`, 1)
			}
			if c.invalidDependency {
				files["/dependency/index.ts"] = "export interface Item { value: number; } function local(): number { return 1; } export function value() { return local(); }"
			}
			fs := &projectReferenceDeclarationReadFS{
				FS:    &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)},
				reads: make(map[tspath.RootedFilePath]int),
			}
			program := newProjectReferenceDeclarationProgram(t, fs, "/consumer/tsconfig.json", ProgramOptions{})
			success := c.isolated && !c.invalidDependency
			for _, project := range []string{"producer", "dependency"} {
				source := tspath.RootedFilePathFromNormalized("/" + project + "/index.ts")
				output := tspath.RootedFilePathFromNormalized("/" + project + "/dist/index.d.ts")
				file := program.GetSourceFile(output)
				assert.Equal(t, file != nil, success)
				assert.Assert(t, program.GetSourceFile(source) == nil)
				assert.Assert(t, !fs.FileExists(output), "generation must not write producer outputs")
				if success {
					assert.Equal(t, file.IsDeclarationFile, true)
					assert.Equal(t, fs.reads[source], 1, "each producer must be parsed only once")
				}
			}
			diagnostics := projectReferenceDeclarationDiagnostics(program)
			if success {
				assert.Equal(t, len(diagnostics), 1, "%v", diagnostics)
				assert.Equal(t, diagnostics[0].Code(), int32(2322))
				assert.Equal(t, diagnostics[0].File().FileName(), tspath.RootedFilePath("/consumer/index.ts"))
			} else {
				assert.Assert(t, slices.ContainsFunc(diagnostics, func(d *ast.Diagnostic) bool { return d.Code() == 6305 }), "%v", diagnostics)
			}
		})
	}
}

func TestProjectReferenceDeclarationGraphUsesResolvedConfig(t *testing.T) {
	t.Parallel()
	files := projectReferenceDeclarationGraphFixture(t)
	files["/dependency/index.ts"] = "export const enum Value { One = 1 }"
	files["/producer/index.ts"] = "import { Value } from '../dependency/index'; export const value: number = Value.One;"
	files["/producer/tsconfig.json"] = strings.Replace(files["/producer/tsconfig.json"], `"strict":true`, `"strict":true,"isolatedModules":true`, 1)
	files["/consumer/index.ts"] = "import { value } from '../producer/index'; const result: number = value;"
	fs := &declarationProducerReadOnlyFS{vfstest.FromMap(files, tspath.CaseSensitive)}
	dependency := parseDeclarationProducerConfig(t, fs, "/dependency/tsconfig.json")
	// The host's resolved configuration intentionally differs from the file.
	// A temporary producer that reparses it loses the preserved-enum exemption
	// for ambient enums and fails noEmitOnError with TS2748.
	dependency.CompilerOptions().PreserveConstEnums = core.TSTrue
	host := &declarationProducerHost{
		CompilerHost: NewCompilerHost(fs, "/", nil, nil, nil),
		producer:     dependency,
	}
	program := NewProgram(ProgramOptions{
		Config:         parseDeclarationProducerConfig(t, fs, "/consumer/tsconfig.json"),
		Host:           host,
		SingleThreaded: core.TSTrue,
	})
	assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") != nil)
	reference := program.GetProjectReferenceFromOutputDts("/dependency/dist/index.d.ts")
	assert.Assert(t, reference != nil)
	assert.Equal(t, reference.Resolved, dependency)
	assert.Equal(t, len(projectReferenceDeclarationDiagnostics(program)), 0)
	assert.Assert(t, !fs.FileExists("/producer/dist/index.d.ts"))
	assert.Assert(t, !fs.FileExists("/dependency/dist/index.d.ts"))
}
