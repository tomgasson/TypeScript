package compiler

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type pausedEmitBenchmarkCheckerPool struct {
	program  *Program
	released chan weak.Pointer[checker.Checker]
}

func (p *pausedEmitBenchmarkCheckerPool) GetChecker(_ context.Context, file *ast.SourceFile) (*checker.Checker, func()) {
	c, _ := checker.NewChecker(p.program, nil)
	if file.FileName() == "/src/blocked.ts" {
		return c, func() {}
	}
	reference := weak.Make(c)
	// The pool owns no strong checker reference after returning it to the caller.
	return c, func() { p.released <- reference }
}

// Run explicitly with -benchtime=1x. This measures the heap retained while one
// output write is paused after 32 other workers released their checkers. The
// disposable pool isolates emitter ownership; a production pool may retain these
// checkers independently. This is not peak heap, RSS, or steady-state LSP memory.
func BenchmarkEmitCompletedWorkerRetention(b *testing.B) {
	if b.N != 1 {
		b.Skip("paused-emit heap sampling requires -benchtime=1x")
	}
	const completedFiles = 32
	files := make(map[string]string, completedFiles+1)
	fileNames := make([]tspath.RootedFilePath, 0, completedFiles+1)
	var source strings.Builder
	for i := range 20 {
		fmt.Fprintf(&source, "export function read%d(value: number): number { return value + %d; }\n", i, i)
	}
	for i := range completedFiles {
		name := tspath.RootedFilePath(fmt.Sprintf("/src/file%d.ts", i))
		files[name.AsString()] = source.String()
		fileNames = append(fileNames, name)
	}
	files["/src/blocked.ts"] = "export const blocked: number = 1;"
	fileNames = append(fileNames, "/src/blocked.ts")
	fs := vfstest.FromMap(files, tspath.CaseSensitive)
	released := make(chan weak.Pointer[checker.Checker], completedFiles)
	options := &core.CompilerOptions{
		NoLib:                core.TSTrue,
		Declaration:          core.TSTrue,
		IsolatedDeclarations: core.TSTrue,
		OutDir:               "/out",
	}
	p := NewProgram(ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(options, fileNames, nil, "/src", fs.CaseSensitivity()),
		Host:           NewCompilerHost(fs, "/", nil, nil, nil),
		SingleThreaded: core.TSFalse,
		CreateCheckerPool: func(p *Program) CheckerPool {
			return &pausedEmitBenchmarkCheckerPool{program: p, released: released}
		},
	})
	p.BindSourceFiles()
	// Warm process-wide checker initialization, then discard that checker. The
	// Program, bound input ASTs, and fixture remain present in both snapshots.
	checker.NewChecker(p, nil)
	blocked := make(chan struct{})
	unblock := make(chan struct{})
	finished := make(chan struct{})
	var result *EmitResult
	references := make([]weak.Pointer[checker.Checker], 0, completedFiles)
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	go func() {
		result = p.Emit(context.Background(), EmitOptions{
			EmitOnly:  EmitOnlyDts,
			ForceEmit: true,
			WriteFile: func(fileName tspath.RootedFilePath, _ string, _ *WriteFileData) error {
				if fileName == "/out/blocked.d.ts" {
					close(blocked)
					<-unblock
				}
				return nil
			},
		})
		close(finished)
	}()
	defer func() {
		close(unblock)
		<-finished
		if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 {
			b.Errorf("unexpected emit result: %+v", result)
		}
	}()
	select {
	case <-blocked:
	case <-finished:
		b.Fatal("emit completed without reaching the blocked output")
	}
	for range completedFiles {
		select {
		case reference := <-released:
			references = append(references, reference)
		case <-finished:
			b.Fatal("emit completed before all other workers released their checkers")
		}
	}
	b.StopTimer()
	// Release callbacks run as worker defers. Allow the callers to return before
	// collecting, and clear sync.Pool victims with a second GC.
	runtime.Gosched()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	var liveCompletedCheckers int
	for _, reference := range references {
		if reference.Value() != nil {
			liveCompletedCheckers++
		}
	}
	runtime.KeepAlive(p)
	b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "live-B/paused-emit")
	b.ReportMetric(float64(liveCompletedCheckers), "live-completed-checkers")
}
