package transpile

import (
	"reflect"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestTranspileExecutionOptionsPreserveOutput(t *testing.T) {
	t.Parallel()
	for _, declaration := range []bool{false, true} {
		name := "module"
		transpile := TranspileModule
		if declaration {
			name = "declaration"
			transpile = TranspileDeclaration
		}
		t.Run(name, func(t *testing.T) {
			for _, c := range []struct {
				name, source string
			}{
				{"annotated", "export class Value { read(input: number): number { return input + 1; } }"},
				{"inferred return", "function internal(): number { return 1; } export function read() { return internal(); }"},
			} {
				t.Run(c.name, func(t *testing.T) {
					options := &core.CompilerOptions{Target: core.ScriptTargetES2015}
					if declaration {
						options.DeclarationMap = core.TSTrue
					} else {
						options.SourceMap = core.TSTrue
					}
					baseline := transpile(t.Context(), c.source, Options{CompilerOptions: options, ReportDiagnostics: true})
					assert.Assert(t, baseline != nil && baseline.OutputText != "" && baseline.SourceMapText != "")
					if declaration && c.name == "inferred return" {
						assert.Equal(t, len(baseline.Diagnostics), 1)
						assert.Equal(t, baseline.Diagnostics[0].Code(), int32(9013))
					} else {
						assert.Equal(t, len(baseline.Diagnostics), 0)
					}
					for _, singleThreaded := range []core.Tristate{core.TSUnknown, core.TSFalse, core.TSTrue} {
						checkerCount := 8
						options.SingleThreaded = singleThreaded
						options.Checkers = &checkerCount
						before := options.Clone()
						actual := transpile(t.Context(), c.source, Options{CompilerOptions: options, ReportDiagnostics: true})
						assert.Assert(t, actual != nil)
						assert.Equal(t, actual.OutputText, baseline.OutputText)
						assert.Equal(t, actual.SourceMapText, baseline.SourceMapText)
						assert.Assert(t, slices.EqualFunc(actual.Diagnostics, baseline.Diagnostics, ast.EqualDiagnostics))
						assert.Assert(t, reflect.DeepEqual(options, before), "transpilation must not modify caller options")
					}
				})
			}
		})
	}
}
