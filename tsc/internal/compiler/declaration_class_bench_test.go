package compiler

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

func declarationClassBenchmarkCases() []declarationBenchmarkCase {
	var cases []declarationBenchmarkCase
	for _, count := range []int{1, 200} {
		var annotated, fullSignature strings.Builder
		for i := range count {
			fmt.Fprintf(&annotated, "export class Value%d { value: number = 1; read(input: number): number { return input + this.value; } }\n", i)
			fmt.Fprintf(&fullSignature, "export class Value%d { /** @type {<T>(input: T) => T} */ read(input) { return input; } }\n", i)
		}
		cases = append(cases,
			declarationBenchmarkCase{
				name:     fmt.Sprintf("AnnotatedTS_%d", count),
				fileName: "/dev/src/index.ts",
				source:   annotated.String(),
			},
			declarationBenchmarkCase{
				name:     fmt.Sprintf("JSFullSignature_%d", count),
				fileName: "/dev/src/index.js",
				source:   fullSignature.String(),
				js:       true,
			},
		)
	}
	return cases
}

// Annotated classes require no inferred index signatures. The JSDoc control
// still needs node building to serialize generic full signatures.
func BenchmarkDeclarationClasses(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}
	ctx := context.Background()
	for _, c := range declarationClassBenchmarkCases() {
		b.Run(c.name, func(b *testing.B) {
			options := declarationBenchmarkOptions(c)
			b.Run("WarmDiagnosticsUncached", func(b *testing.B) {
				p := NewProgram(options)
				file := p.GetSourceFile(c.fileName)
				if file == nil {
					b.Fatal("benchmark source was not loaded")
				}
				checker, done := p.GetTypeCheckerForFile(ctx, file)
				defer done()
				host := newEmitHost(p, checker)
				if diags := getDeclarationDiagnostics(host, file); len(diags) != 0 {
					b.Fatalf("unexpected declaration diagnostics: %v", diags)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					getDeclarationDiagnostics(host, file)
				}
			})
			b.Run("WarmEmit", func(b *testing.B) {
				p := NewProgram(options)
				emitOptions := EmitOptions{
					EmitOnly: EmitOnlyDts,
					WriteFile: func(tspath.RootedFilePath, string, *WriteFileData) error {
						return nil
					},
				}
				result := p.Emit(ctx, emitOptions)
				if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 || len(result.EmittedFiles) != 1 {
					b.Fatalf("unexpected declaration emit result: %+v", result)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					p.Emit(ctx, emitOptions)
				}
			})
		})
	}
}
