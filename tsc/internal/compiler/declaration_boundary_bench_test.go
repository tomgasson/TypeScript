package compiler

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

const declarationBoundaryModules = 48

type declarationBoundaryFixture struct {
	fs                 vfs.FS
	producer, consumer *tsoptions.ParsedCommandLine
	sourceBytes        int
}

// Both arms retain the same source fixture. Only the declaration arm adds
// generated output to this in-memory FS; no filesystem-backed emit is involved.
func newDeclarationBoundaryFixture(tb testing.TB) declarationBoundaryFixture {
	tb.Helper()
	files := map[string]string{
		"/globals.d.ts":           `interface Object {} interface Function {} interface CallableFunction {} interface NewableFunction {} interface IArguments {} interface String {} interface Number {} interface Boolean {} interface RegExp {} interface Array<T> { length: number; [n: number]: T; }`,
		"/consumer/tsconfig.json": `{"compilerOptions":{"noLib":true,"strict":true,"noEmit":true,"module":"esnext","moduleResolution":"bundler","target":"es2020"},"files":["index.ts","../globals.d.ts"],"references":[{"path":"../producer"}]}`,
	}
	var barrel, consumer, roots strings.Builder
	roots.WriteString(`"src/index.ts","../globals.d.ts"`)
	var sourceBytes int
	for i := range declarationBoundaryModules {
		var source strings.Builder
		fmt.Fprintf(&source, "export interface Value%d { value: number; }\nexport function read%d(input: Value%d): number {\n", i, i, i)
		for j := range 256 {
			fmt.Fprintf(&source, "  const local%d: number = input.value + %d;\n", j, j)
		}
		source.WriteString("  return local255;\n}\n")
		files[fmt.Sprintf("/producer/src/module%d.ts", i)] = source.String()
		sourceBytes += source.Len()
		fmt.Fprintf(&roots, `,"src/module%d.ts"`, i)
		fmt.Fprintf(&barrel, "export * from './module%d';\n", i)
		fmt.Fprintf(&consumer, "import { read%d, type Value%d } from '../producer/src/index';\nconst input%d: Value%d = { value: %d };\nconst result%d: number = read%d(input%d);\n", i, i, i, i, i, i, i, i)
	}
	// A nonempty diagnostic result checks more than merely successful loading.
	consumer.WriteString("const mismatch: string = read0(input0);\n")
	files["/producer/src/index.ts"] = barrel.String()
	sourceBytes += barrel.Len()
	files["/consumer/index.ts"] = consumer.String()
	files["/producer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"declaration":true,"isolatedDeclarations":true,"module":"esnext","moduleResolution":"bundler","target":"es2020","rootDir":"src","declarationDir":"types"},"files":[` + roots.String() + `]}`
	fs := vfstest.FromMap(files, tspath.CaseSensitive)
	parse := func(name tspath.RootedFilePath) *tsoptions.ParsedCommandLine {
		config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(name, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
		if config == nil || len(diagnostics) != 0 || len(config.Errors) != 0 {
			tb.Fatalf("invalid benchmark config %s: %v", name, diagnostics)
		}
		return config
	}
	return declarationBoundaryFixture{fs: fs, producer: parse("/producer/tsconfig.json"), consumer: parse("/consumer/tsconfig.json"), sourceBytes: sourceBytes}
}

func (f declarationBoundaryFixture) newConsumer(useSource bool) *Program {
	return NewProgram(ProgramOptions{
		Config: f.consumer, Host: NewCompilerHost(f.fs, "/", nil, nil, nil),
		SingleThreaded: core.TSTrue, UseSourceOfProjectReference: useSource,
	})
}

// Produce declarations using the producer's complete parsed config. The writer
// copies only filenames/text into the in-memory FS: no Program, nodes,
// WriteFileData, or EmitResult survives this call. This does not semantically
// validate producer bodies; it measures declaration production and validation.
func (f declarationBoundaryFixture) generate(tb testing.TB) int {
	tb.Helper()
	p := NewProgram(ProgramOptions{Config: f.producer, Host: NewCompilerHost(f.fs, "/", nil, nil, nil), SingleThreaded: core.TSTrue})
	count, bytes := 0, 0
	result := p.Emit(context.Background(), EmitOptions{
		EmitOnly: EmitOnlyDts,
		WriteFile: func(name tspath.RootedFilePath, text string, _ *WriteFileData) error {
			if !strings.HasPrefix(name.AsString(), "/producer/types/") || !strings.HasSuffix(name.AsString(), ".d.ts") {
				tb.Fatalf("unexpected output %s", name)
			}
			count++
			bytes += len(text)
			return f.fs.WriteFile(name, text)
		},
	})
	if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 || count != declarationBoundaryModules+1 {
		tb.Fatalf("declaration generation failed: %d outputs, result %+v", count, result)
	}
	return bytes
}

func checkDeclarationBoundary(p *Program) []*ast.Diagnostic {
	return p.GetSemanticDiagnostics(context.Background(), p.GetSourceFile("/consumer/index.ts"))
}

func validateDeclarationBoundary(tb testing.TB, p *Program, useSource bool) []*ast.Diagnostic {
	tb.Helper()
	if p.GetSourceFile("/consumer/index.ts") == nil {
		tb.Fatal("consumer was not loaded")
	}
	for i := range declarationBoundaryModules {
		sourceName := tspath.RootedFilePath(fmt.Sprintf("/producer/src/module%d.ts", i))
		declarationName := tspath.RootedFilePath(fmt.Sprintf("/producer/types/module%d.d.ts", i))
		if useSource {
			file := p.GetSourceFile(sourceName)
			if file == nil || file.IsDeclarationFile || p.GetProjectReferenceFromSource(file.PathKey()) == nil || p.GetSourceFile(declarationName) != nil {
				tb.Fatalf("source project-reference route failed for %s", sourceName)
			}
		} else if file := p.GetSourceFile(declarationName); file == nil || !file.IsDeclarationFile || p.GetSourceFile(sourceName) != nil {
			tb.Fatalf("declaration project-reference route failed for %s", declarationName)
		}
	}
	ctx := context.Background()
	if diagnostics := slices.Concat(p.GetConfigFileParsingDiagnostics(), p.GetProgramDiagnostics(), p.GetGlobalDiagnostics(ctx), p.GetSyntacticDiagnostics(ctx, nil)); len(diagnostics) != 0 {
		tb.Fatalf("unexpected non-semantic diagnostics: %v", diagnostics)
	}
	diagnostics := checkDeclarationBoundary(p)
	if len(diagnostics) != 1 || diagnostics[0].Code() != 2322 || diagnostics[0].File().FileName() != "/consumer/index.ts" {
		tb.Fatalf("expected one consumer TS2322: %v", diagnostics)
	}
	return diagnostics
}

func TestDeclarationBoundaryBenchmarkFixture(t *testing.T) {
	f := newDeclarationBoundaryFixture(t)
	sourceDiagnostics := validateDeclarationBoundary(t, f.newConsumer(true), true)
	outputBytes := f.generate(t)
	declarationDiagnostics := validateDeclarationBoundary(t, f.newConsumer(false), false)
	if !slices.EqualFunc(sourceDiagnostics, declarationDiagnostics, ast.EqualDiagnostics) {
		t.Fatal("source and declaration consumer diagnostics differ")
	}
	t.Logf("producer: %d modules plus barrel, %d source bytes, %d declaration bytes", declarationBoundaryModules, f.sourceBytes, outputBytes)
	diagnostic := sourceDiagnostics[0]
	t.Logf("matching diagnostic: %s at [%d,%d), TS%d: %s", diagnostic.File().FileName(), diagnostic.Pos(), diagnostic.End(), diagnostic.Code(), diagnostic.String())
}

// A cold operation creates a new host, Program, graph, parser and checker. The
// source VFS and top-level parsed configs are shared; referenced configs are
// resolved normally. This is focused consumer checking, not a whole build.
// ReusedDeclarations excludes production; GenerateAndCheck charges it, while
// GenerateDeclarations separately exposes the numerator of a reuse break-even
// estimate: generation / (source consumer - declaration consumer).
// Generation replaces output in an already populated in-memory destination;
// the separate retained-heap benchmark includes initial output creation.
func BenchmarkDeclarationBoundary(b *testing.B) {
	for _, mode := range []string{"SourceConsumer", "ReusedDeclarations", "GenerateDeclarations", "GenerateAndCheck"} {
		b.Run(mode, func(b *testing.B) {
			f := newDeclarationBoundaryFixture(b)
			useSource := mode == "SourceConsumer"
			if !useSource {
				f.generate(b)
			}
			validateDeclarationBoundary(b, f.newConsumer(useSource), useSource)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if mode == "GenerateDeclarations" || mode == "GenerateAndCheck" {
					f.generate(b)
				}
				if mode != "GenerateDeclarations" {
					checkDeclarationBoundary(f.newConsumer(useSource))
				}
			}
		})
	}
}

// Run separately with -benchtime=1x. Each operation retains eight independent
// consumer Programs over one shared source fixture. For the declaration arm,
// the output is produced AFTER the baseline and stored in the same in-memory
// FS, so its retained text/storage is included once for the batch. No producer
// Program is retained. Both arms exclude the identical common source fixture
// and parsed top-level configs. This is marginal post-GC heap, not peak/RSS or
// steady-state LSP memory; live-B/consumer amortizes the shared output over 8.
func BenchmarkDeclarationBoundaryRetainedHeap(b *testing.B) {
	for _, useSource := range []bool{true, false} {
		name := core.IfElse(useSource, "SourceConsumer", "Declarations")
		b.Run(name, func(b *testing.B) {
			if b.N != 1 {
				b.Skip("retained-heap sampling requires -benchtime=1x")
			}
			// Discard a separate warmup fixture, including any generated text.
			warm := newDeclarationBoundaryFixture(b)
			if !useSource {
				warm.generate(b)
			}
			validateDeclarationBoundary(b, warm.newConsumer(useSource), useSource)
			warm = declarationBoundaryFixture{}
			f := newDeclarationBoundaryFixture(b)
			const batchSize = 8
			programs := make([]*Program, batchSize)
			runtime.GC()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ResetTimer()
			outputBytes := 0
			if !useSource {
				outputBytes = f.generate(b)
			}
			for i := range programs {
				programs[i] = f.newConsumer(useSource)
				checkDeclarationBoundary(programs[i])
			}
			b.StopTimer()
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(programs)
			runtime.KeepAlive(f)
			live := float64(int64(after.HeapAlloc) - int64(before.HeapAlloc))
			b.ReportMetric(live, "live-B/batch")
			b.ReportMetric(live/batchSize, "live-B/consumer")
			b.ReportMetric(float64(outputBytes), "shared-declaration-B")
		})
	}
}
