package compiler

import (
	"context"
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type declarationEligibilityBenchmarkCase struct {
	name    string
	target  tspath.RootedFilePath
	options ProgramOptions
}

func declarationEligibilityBenchmarkCases(b *testing.B) []declarationEligibilityBenchmarkCase {
	var cases []declarationEligibilityBenchmarkCase
	for _, c := range []struct {
		name, source string
		fileName     tspath.RootedFilePath
		options      core.CompilerOptions
	}{
		{
			name:     "JSON",
			fileName: "/src/data.json",
			source:   `{"value": 1}`,
			options:  core.CompilerOptions{ResolveJsonModule: core.TSTrue},
		},
		{
			name:     "SkippedJS",
			fileName: "/src/index.js",
			source:   `export function read(value) { return value + 1; }`,
			options:  core.CompilerOptions{AllowJs: core.TSTrue, NoEmitForJsFiles: core.TSTrue},
		},
		{
			// Declaration files already return before checker acquisition. Keep
			// this control to distinguish the change from general run-to-run drift.
			name:     "DeclarationFile",
			fileName: "/src/index.d.ts",
			source:   `export declare function read(value: number): number;`,
		},
		{
			// This file is eligible for declaration transformation. Its warm
			// requests exercise the normal diagnostic-cache hit path.
			name:     "EligibleTS",
			fileName: "/src/index.ts",
			source:   `export function read(value: number): number { return value + 1; }`,
			options:  core.CompilerOptions{IsolatedDeclarations: core.TSTrue},
		},
	} {
		fs := bundled.WrapFS(vfstest.FromMap(map[string]string{c.fileName.AsString(): c.source}, tspath.CaseSensitive))
		c.options.Target = core.ScriptTargetES2015
		c.options.Module = core.ModuleKindESNext
		c.options.ModuleResolution = core.ModuleResolutionKindBundler
		c.options.Declaration = core.TSTrue
		c.options.OutDir = "/out"
		cases = append(cases, declarationEligibilityBenchmarkCase{
			name:   c.name,
			target: c.fileName,
			options: ProgramOptions{
				Config:         tsoptions.NewParsedCommandLine(&c.options, []tspath.RootedFilePath{c.fileName}, nil, "/src", fs.CaseSensitivity()),
				Host:           NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil),
				SingleThreaded: core.TSTrue,
			},
		})
	}

	// Exercise the normal project-reference source redirection and package
	// resolution paths; the producer's declaration output does not exist.
	fs := bundled.WrapFS(vfstest.FromMap(map[string]any{
		"/src/tsconfig.json":          `{"compilerOptions":{"declaration":true,"isolatedDeclarations":true,"module":"nodenext","outDir":"dist"},"files":["index.ts"],"references":[{"path":"../reference"}]}`,
		"/src/index.ts":               `import { read0 } from "reference"; export const result: number = read0({ value: 1 });`,
		"/src/node_modules/reference": vfstest.Symlink("/reference"),
		"/reference/tsconfig.json":    `{"compilerOptions":{"composite":true,"isolatedDeclarations":true,"outDir":"dist"},"files":["index.ts"]}`,
		"/reference/package.json":     `{"name":"reference","version":"1.0.0","types":"dist/index.d.ts"}`,
		"/reference/index.ts":         declarationBenchmarkCases()[1].source,
	}, tspath.CaseSensitive))
	config, diags := tsoptions.GetParsedCommandLineOfConfigFile("/src/tsconfig.json", nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
	if len(diags) != 0 {
		b.Fatalf("unexpected project-reference config diagnostics: %v", diags)
	}
	cases = append(cases, declarationEligibilityBenchmarkCase{
		name:   "ReferenceSource",
		target: "/reference/index.ts",
		options: ProgramOptions{
			Config:                      config,
			Host:                        NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil),
			UseSourceOfProjectReference: true,
			SingleThreaded:              core.TSTrue,
		},
	})
	return cases
}

func validateDeclarationEligibilityBenchmark(b *testing.B, c declarationEligibilityBenchmarkCase, p *Program) {
	file := p.GetSourceFile(c.target)
	if file == nil {
		b.Fatalf("benchmark source %s was not loaded", c.target)
	}
	if c.name == "ReferenceSource" && p.GetProjectReferenceFromSource(file.PathKey()) == nil {
		b.Fatal("benchmark source was not associated with its referenced project")
	}
	if diags := p.GetDeclarationDiagnostics(context.Background(), file); len(diags) != 0 {
		b.Fatalf("unexpected declaration diagnostics: %v", diags)
	}
}

func BenchmarkDeclarationEligibility(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}
	ctx := context.Background()
	for _, c := range declarationEligibilityBenchmarkCases(b) {
		b.Run(c.name, func(b *testing.B) {
			b.Run("ColdProgramDiagnostics", func(b *testing.B) {
				validateDeclarationEligibilityBenchmark(b, c, NewProgram(c.options))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					p := NewProgram(c.options)
					p.GetDeclarationDiagnostics(ctx, p.GetSourceFile(c.target))
				}
			})
			b.Run("WarmDiagnostics", func(b *testing.B) {
				p := NewProgram(c.options)
				validateDeclarationEligibilityBenchmark(b, c, p)
				file := p.GetSourceFile(c.target)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					// The first request above populated any diagnostic cache.
					p.GetDeclarationDiagnostics(ctx, file)
				}
			})
		})
	}
}

// Run separately with -benchtime=1x. Each operation retains a batch of 16
// Programs; live-B/program alone is normalized per Program. This is post-GC
// marginal process heap, including parsed libraries, and is not RSS or peak heap.
func BenchmarkDeclarationEligibilityRetainedHeap(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}
	for _, c := range declarationEligibilityBenchmarkCases(b) {
		b.Run(c.name, func(b *testing.B) {
			if b.N != 1 {
				b.Skip("retained-heap sampling requires -benchtime=1x")
			}
			const batchSize = 16
			validateDeclarationEligibilityBenchmark(b, c, NewProgram(c.options))
			programs := make([]*Program, batchSize)
			// The shared fixture, filesystem, and configuration exist in both
			// snapshots. Discard warmup state and clear sync.Pool victims.
			runtime.GC()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ResetTimer()
			for i := range programs {
				programs[i] = NewProgram(c.options)
				validateDeclarationEligibilityBenchmark(b, c, programs[i])
			}
			b.StopTimer()
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(programs)
			runtime.KeepAlive(c)
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc))/batchSize, "live-B/program")
		})
	}
}
