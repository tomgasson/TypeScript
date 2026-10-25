package compiler

import (
	"context"
	"maps"
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

func TestProjectReferenceDeclarationCaptureValidation(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		source     string
		other      string
		noCheck    bool
		missingLib bool
		allowJs    bool
		noEmit     bool
		wantOutput bool
		wantCodes  []int32
	}{
		{name: "declarations and maps", source: "export function value(input: number): number { return input; }", wantOutput: true},
		{name: "implementation error", source: "export const value: number = 'wrong';", wantCodes: []int32{2322}},
		{name: "unrelated implementation error", source: "export const value: number = 1;", other: "export const other: number = 'wrong';", wantCodes: []int32{2322}},
		{name: "global error", source: "export const value: number = 1;", missingLib: true, wantCodes: []int32{2318}},
		{name: "program option error", source: "export const value: number = 1;", allowJs: true, wantCodes: []int32{5053}},
		{name: "syntax error", source: "export const value: number = ;", wantCodes: []int32{1109}},
		{name: "isolated declaration error", source: "function local(): number { return 1; } export function value() { return local(); }", wantCodes: []int32{9013}},
		{name: "unrelated isolated declaration error", source: "export const value: number = 1;", other: "function local(): number { return 1; } export function other() { return local(); }", wantCodes: []int32{9013}},
		{name: "noCheck preserves implementation error allowance", source: "export const value: number = 'wrong';", noCheck: true, wantOutput: true},
		{name: "noCheck retains declaration errors", source: "function local(): number { return 1; } export function value() { return local(); }", noCheck: true, wantCodes: []int32{9013}},
		{name: "noEmit fallback", source: "export const value: number = 1;", noEmit: true},
		// Binding the IIFE body records the property assignment on local. Even
		// noCheck with isolatedDeclarations must preserve that TS9023 diagnostic;
		// skipping implementation bodies would silently lose it.
		{name: "nested expando inference", source: "export const value = (() => { function local(): void {} local.extra = 1; return local; })();", noCheck: true, wantCodes: []int32{9010, 9023}},
	} {
		t.Run(c.name, func(t *testing.T) {
			newProgram := func() *Program {
				globals := "interface Object {} interface Function {} interface CallableFunction {} interface NewableFunction {} interface IArguments {} interface String {} interface Number {} interface Boolean {} interface RegExp {} interface Array<T> { length: number; [n: number]: T; }"
				if c.missingLib {
					globals = strings.Replace(globals, "interface Boolean {}", "", 1)
				}
				other := c.other
				if other == "" {
					other = "export const other: number = 1;"
				}
				fs := &declarationProducerReadOnlyFS{vfstest.FromMap(map[string]string{
					"/globals.d.ts": globals, "/producer/index.ts": c.source, "/producer/other.ts": other,
				}, tspath.CaseSensitive)}
				options := &core.CompilerOptions{
					NoLib: core.TSTrue, Strict: core.TSTrue, Declaration: core.TSTrue, DeclarationMap: core.TSTrue,
					IsolatedDeclarations: core.TSTrue, NoEmitOnError: core.TSTrue,
					AllowJs: core.IfElse(c.allowJs, core.TSTrue, core.TSFalse),
					NoCheck: core.IfElse(c.noCheck, core.TSTrue, core.TSFalse), NoEmit: core.IfElse(c.noEmit, core.TSTrue, core.TSFalse),
					Module: core.ModuleKindESNext, RootDir: "/producer", DeclarationDir: "/types",
				}
				return NewProgram(ProgramOptions{
					Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/globals.d.ts", "/producer/index.ts", "/producer/other.ts"}, nil, "/producer", fs.CaseSensitivity()),
					Host:   NewCompilerHost(fs, "/", nil, nil, nil), SingleThreaded: core.TSTrue,
				})
			}
			ordinary := newProgram()
			want, result := captureProducerDeclarations(ordinary, nil)
			codes := core.Map(result.Diagnostics, func(d *ast.Diagnostic) int32 { return d.Code() })
			slices.Sort(codes)
			assert.Assert(t, slices.Equal(codes, c.wantCodes), "ordinary emit diagnostic codes: %v; want %v", codes, c.wantCodes)
			if result.EmitSkipped || len(result.Diagnostics) != 0 {
				want = nil
			}
			optimized := newProgram()
			got := emitProjectReferenceDeclarations(optimized)
			assert.Equal(t, len(got) != 0, c.wantOutput)
			assert.Assert(t, maps.Equal(got, want), "captured declarations and maps differ from ordinary emit")
			assert.Equal(t, got == nil, want == nil, "failed capture must discard every output")

			// Actual emit collects declaration errors itself. A separate diagnostic
			// transform would populate this cache and repeat work before printing.
			if c.wantOutput {
				file := optimized.GetSourceFile("/producer/index.ts")
				_, cached := optimized.declarationDiagnosticCache.Load(file)
				assert.Assert(t, !cached, "capture must not run a separate declaration-diagnostic transform")
				_, cached = ordinary.declarationDiagnosticCache.Load(ordinary.GetSourceFile("/producer/index.ts"))
				assert.Assert(t, cached, "ordinary noEmitOnError emit validates declarations before printing")
				assert.Equal(t, len(got), 4)
			}

			ctx := context.Background()
			left := SortAndDeduplicateDiagnostics(ordinary.GetSemanticDiagnostics(ctx, nil))
			right := SortAndDeduplicateDiagnostics(optimized.GetSemanticDiagnostics(ctx, nil))
			assert.Assert(t, slices.EqualFunc(left, right, ast.EqualDiagnostics), "semantic diagnostics changed after capture")
			left = SortAndDeduplicateDiagnostics(ordinary.GetDeclarationDiagnostics(ctx, nil))
			right = SortAndDeduplicateDiagnostics(optimized.GetDeclarationDiagnostics(ctx, nil))
			assert.Assert(t, slices.EqualFunc(left, right, ast.EqualDiagnostics), "declaration diagnostics changed after capture")
			left = ordinary.GetGlobalDiagnostics(ctx)
			right = optimized.GetGlobalDiagnostics(ctx)
			assert.Assert(t, slices.EqualFunc(left, right, ast.EqualDiagnostics), "global diagnostics changed after capture")
		})
	}
}
