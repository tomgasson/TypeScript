package compiler

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type declarationBenchmarkCase struct {
	name     string
	fileName tspath.RootedFilePath
	source   string
	js       bool
	commonJS bool
}

func declarationBenchmarkCases() []declarationBenchmarkCase {
	var cases []declarationBenchmarkCase
	for _, count := range []int{1, 200} {
		var source strings.Builder
		for i := range count {
			fmt.Fprintf(&source, "export interface Value%d { value: number; }\nexport function read%d(input: Value%d): number { const doubled = input.value * 2; return doubled + 1; }\n", i, i, i)
		}
		cases = append(cases, declarationBenchmarkCase{
			name:     fmt.Sprintf("AnnotatedTS_%d", count),
			fileName: "/dev/src/index.ts",
			source:   source.String(),
		})
	}
	cases = append(cases, declarationBenchmarkCase{
		name:     "JSExpando",
		fileName: "/dev/src/index.js",
		source:   "/** @param {number} value @returns {number} */\nexport function read(value) { return value + 1; }\nread.version = 1;\n",
		js:       true,
	})
	return append(cases, declarationBenchmarkCase{
		name:     "JSCommonJS",
		fileName: "/dev/src/index.js",
		source:   "/** @param {number} value @returns {number} */\nfunction read(value) { return value + 1; }\nread.version = 1;\nmodule.exports = read;\n",
		js:       true,
		commonJS: true,
	})
}

func declarationBenchmarkOptions(c declarationBenchmarkCase) ProgramOptions {
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{c.fileName.AsString(): c.source}, tspath.CaseSensitive))
	opts := &core.CompilerOptions{
		Target:               core.ScriptTargetES2015,
		Module:               core.IfElse(c.commonJS, core.ModuleKindCommonJS, core.ModuleKindESNext),
		Declaration:          core.TSTrue,
		IsolatedDeclarations: core.IfElse(c.js, core.TSFalse, core.TSTrue),
		AllowJs:              core.IfElse(c.js, core.TSTrue, core.TSFalse),
		OutDir:               "/dev/out",
	}
	return ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(opts, []tspath.RootedFilePath{c.fileName}, nil, "/dev/src", fs.CaseSensitivity()),
		Host:           NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil),
		SingleThreaded: core.TSTrue,
	}
}

// Run separately with -bench '^BenchmarkDeclarationRetainedHeap$' -benchtime=1x.
// This samples live Go heap after GC, not allocated bytes, RSS, or peak heap.
func BenchmarkDeclarationRetainedHeap(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}
	if b.N != 1 {
		b.Skip("retained-heap sampling requires -benchtime=1x")
	}
	const batchSize = 16
	c := declarationBenchmarkCases()[1] // AnnotatedTS_200
	options := declarationBenchmarkOptions(c)
	ctx := context.Background()
	newCheckedProgram := func() *Program {
		p := NewProgram(options)
		file := p.GetSourceFile(c.fileName)
		if file == nil {
			b.Fatal("benchmark source was not loaded")
		}
		if diags := p.GetDeclarationDiagnostics(ctx, file); len(diags) != 0 {
			b.Fatalf("unexpected declaration diagnostics: %v", diags)
		}
		return p
	}
	// Initialize process-wide caches, then discard the warmup program. The
	// fixture, filesystem, and parsed configuration remain shared by the batch
	// and are present in both snapshots. Two GCs also clear sync.Pool victims.
	newCheckedProgram()
	programs := make([]*Program, batchSize)
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for i := range programs {
		programs[i] = newCheckedProgram()
	}
	b.StopTimer()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(programs)
	runtime.KeepAlive(options)
	b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc))/batchSize, "live-B/program")
}

func BenchmarkDeclaration(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}
	ctx := context.Background()
	for _, c := range declarationBenchmarkCases() {
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
					// Bypass Program.declarationDiagnosticCache while retaining the
					// same parsed source and warm checker, as for repeated transforms.
					getDeclarationDiagnostics(host, file)
				}
			})
			b.Run("WarmEmit", func(b *testing.B) {
				p := NewProgram(options)
				emitOptions := EmitOptions{
					EmitOnly: EmitOnlyDts,
					WriteFile: func(_ tspath.RootedFilePath, _ string, _ *WriteFileData) error {
						return nil
					},
				}
				result := p.Emit(ctx, emitOptions)
				if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 {
					b.Fatalf("unexpected declaration emit result: %+v", result)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					p.Emit(ctx, emitOptions)
				}
			})
			if c.name == "AnnotatedTS_200" {
				b.Run("ColdProgramDiagnostics", func(b *testing.B) {
					p := NewProgram(options)
					file := p.GetSourceFile(c.fileName)
					if file == nil {
						b.Fatal("benchmark source was not loaded")
					}
					if diags := p.GetDeclarationDiagnostics(ctx, file); len(diags) != 0 {
						b.Fatalf("unexpected declaration diagnostics: %v", diags)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						p := NewProgram(options)
						p.GetDeclarationDiagnostics(ctx, p.GetSourceFile(c.fileName))
					}
				})
			}
		})
	}
}
