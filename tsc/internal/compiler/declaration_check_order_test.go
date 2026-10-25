package compiler

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestDeclarationEmitAndSemanticCheckOrder(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		source        string
		js            bool
		semanticCodes []int32
		emitCodes     []int32
		declarations  []string
	}{
		{
			name: "generic class methods",
			source: `export class Box<T extends { value: number }> {
    read<U extends T>(input: U): U { return input; }
    wrong(input: T): number { return "wrong"; }
}`,
			semanticCodes: []int32{2322},
			declarations:  []string{"read<U extends T>(input: U): U;", "wrong(input: T): number;"},
		},
		{
			name: "JSDoc full signatures and overloads",
			js:   true,
			source: `/** @type {<T>(input: T) => T} */
export function identity(input) { return input; }
/** @type {{(value: number): number; (value: string): string}} */
export function multi(value) { return value; }
/**
 * @overload
 * @param {number} value
 * @returns {number}
 */
/**
 * @overload
 * @param {string} value
 * @returns {string}
 */
/**
 * @param {number | string} value
 * @returns {number | string}
 */
export function echo(value) { return value; }
identity(1).missing;
`,
			// A full signature with multiple calls provides no single contextual
			// signature for multi, so its value parameter remains implicit any.
			semanticCodes: []int32{2339, 7006},
			declarations:  []string{"identity<T>", "multi(", "echo(value: number): number;", "echo(value: string): string;"},
		},
		{
			name: "computed overloads and merged namespace",
			source: `export class Reader {
    [Symbol.toStringTag](value: number): number;
    [Symbol.toStringTag](value: string): string;
    [Symbol.toStringTag](value: number | string): number | string { return value; }
}
export function merged<T>(value: T): T { return value; }
export namespace merged { export const version: number = 1; }
export const wrong: number = merged("wrong");
`,
			semanticCodes: []int32{2322},
			declarations:  []string{"[Symbol.toStringTag](value: number): number;", "[Symbol.toStringTag](value: string): string;", "function merged<T>", "namespace merged"},
		},
		{
			name: "invalid isolated return inference",
			source: `function internal(): number { return 1; }
export function value() { return internal(); }
`,
			emitCodes: []int32{9013},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fileName := tspath.RootedFilePath("/src/index.ts")
			if c.js {
				fileName = "/src/index.js"
			}
			type outcome struct {
				semantic []*ast.Diagnostic
				global   []*ast.Diagnostic
				emit     *EmitResult
				output   map[tspath.RootedFilePath]string
			}
			run := func(emitFirst bool) outcome {
				fs := vfstest.FromMap(map[string]string{
					"/globals.d.ts":     `interface Object {} interface Function {} interface CallableFunction {} interface NewableFunction {} interface IArguments {} interface String {} interface Number {} interface Boolean {} interface RegExp {} interface Array<T> { length: number; [n: number]: T; } interface Symbol {} interface SymbolConstructor { readonly toStringTag: unique symbol; } declare var Symbol: SymbolConstructor;`,
					fileName.AsString(): c.source,
				}, tspath.CaseSensitive)
				options := &core.CompilerOptions{
					NoLib:                core.TSTrue,
					Declaration:          core.TSTrue,
					IsolatedDeclarations: core.IfElse(c.js, core.TSFalse, core.TSTrue),
					AllowJs:              core.IfElse(c.js, core.TSTrue, core.TSFalse),
					CheckJs:              core.IfElse(c.js, core.TSTrue, core.TSFalse),
					StrictNullChecks:     core.TSTrue,
					Target:               core.ScriptTargetES2015,
					Module:               core.ModuleKindESNext,
					OutDir:               "/out",
				}
				p := NewProgram(ProgramOptions{
					Config:         tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/globals.d.ts", fileName}, nil, "/src", fs.CaseSensitivity()),
					Host:           NewCompilerHost(fs, "/", nil, nil, nil),
					SingleThreaded: core.TSTrue,
				})
				ctx := context.Background()
				file := p.GetSourceFile(fileName)
				assert.Assert(t, file != nil)
				assert.Equal(t, len(p.GetSyntacticDiagnostics(ctx, nil)), 0)
				result := outcome{output: make(map[tspath.RootedFilePath]string)}
				emit := func() {
					result.emit = p.Emit(ctx, EmitOptions{
						TargetSourceFiles: []*ast.SourceFile{file},
						EmitOnly:          EmitOnlyDts,
						WriteFile: func(path tspath.RootedFilePath, text string, _ *WriteFileData) error {
							result.output[path] = text
							return nil
						},
					})
				}
				if emitFirst {
					emit()
				}
				result.semantic = p.GetSemanticDiagnostics(ctx, file)
				if !emitFirst {
					emit()
				}
				result.global = p.GetGlobalDiagnostics(ctx)
				assert.Equal(t, len(result.global), 0)
				assert.Assert(t, result.emit != nil)
				return result
			}

			emitFirst := run(true)
			checkFirst := run(false)
			compareDiagnostics := func(name string, left, right []*ast.Diagnostic) {
				left = slices.Clone(left)
				right = slices.Clone(right)
				slices.SortFunc(left, ast.CompareDiagnostics)
				slices.SortFunc(right, ast.CompareDiagnostics)
				assert.Assert(t, slices.EqualFunc(left, right, ast.EqualDiagnostics), "%s diagnostics depend on operation order: %v versus %v", name, left, right)
			}
			compareDiagnostics("semantic", emitFirst.semantic, checkFirst.semantic)
			compareDiagnostics("global", emitFirst.global, checkFirst.global)
			compareDiagnostics("declaration", emitFirst.emit.Diagnostics, checkFirst.emit.Diagnostics)
			assert.Equal(t, emitFirst.emit.EmitSkipped, checkFirst.emit.EmitSkipped)
			assert.DeepEqual(t, emitFirst.emit.EmittedFiles, checkFirst.emit.EmittedFiles)
			assert.DeepEqual(t, emitFirst.output, checkFirst.output)
			for _, expected := range []struct {
				diagnostics []*ast.Diagnostic
				codes       []int32
			}{
				{emitFirst.semantic, c.semanticCodes},
				{emitFirst.emit.Diagnostics, c.emitCodes},
			} {
				codes := make([]int32, len(expected.diagnostics))
				for i, diagnostic := range expected.diagnostics {
					codes[i] = diagnostic.Code()
					assert.Equal(t, diagnostic.File().FileName(), fileName)
					t.Logf("matching TS%d at %s [%d,%d): %s", diagnostic.Code(), fileName, diagnostic.Pos(), diagnostic.End(), diagnostic.String())
				}
				slices.Sort(codes)
				assert.Assert(t, slices.Equal(codes, expected.codes), "unexpected diagnostics: %v", expected.diagnostics)
			}
			assert.Equal(t, emitFirst.emit.EmitSkipped, len(c.emitCodes) != 0)
			if len(c.emitCodes) != 0 {
				assert.Equal(t, len(emitFirst.output), 0)
			} else {
				assert.Equal(t, len(emitFirst.output), 1)
				text := emitFirst.output["/out/index.d.ts"]
				for _, declaration := range c.declarations {
					assert.Assert(t, strings.Contains(text, declaration), "missing declaration %q in %s", declaration, text)
				}
			}
		})
	}
}
