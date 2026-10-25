package transpile

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Each operation includes creating the single-file program and its checker.
// Module emit is a control with no declaration support library to load.
func BenchmarkTranspile(b *testing.B) {
	ctx := context.Background()
	for _, declaration := range []bool{false, true} {
		name := "Module"
		transpile := TranspileModule
		if declaration {
			name = "Declaration"
			transpile = TranspileDeclaration
		}
		b.Run(name, func(b *testing.B) {
			for _, count := range []int{1, 200} {
				var source strings.Builder
				for i := range count {
					fmt.Fprintf(&source, "export class Value%d { value: number = 1; read(input: number): number { return input + this.value; } }\n", i)
				}
				input := source.String()
				b.Run(fmt.Sprintf("Annotated_%d", count), func(b *testing.B) {
					options := Options{CompilerOptions: &core.CompilerOptions{Target: core.ScriptTargetES2015}}
					output := transpile(ctx, input, options)
					if output == nil || output.OutputText == "" || len(output.Diagnostics) != 0 {
						b.Fatalf("unexpected transpilation output: %+v", output)
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(input)))
					b.ResetTimer()
					for b.Loop() {
						transpile(ctx, input, options)
					}
				})
			}
		})
	}
}
