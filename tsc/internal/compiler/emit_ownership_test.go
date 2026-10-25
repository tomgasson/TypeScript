package compiler

import (
	"context"
	"runtime"
	"testing"
	"weak"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// This pool releases its only strong checker reference to the caller.
type disposableEmitCheckerPool struct {
	program  *Program
	released chan weak.Pointer[checker.Checker]
}

func (p *disposableEmitCheckerPool) GetChecker(_ context.Context, file *ast.SourceFile) (*checker.Checker, func()) {
	c, _ := checker.NewChecker(p.program, nil)
	if file.FileName() == "/src/first.ts" {
		reference := weak.Make(c)
		return c, func() { p.released <- reference }
	}
	return c, func() {}
}

func TestEmitReleasesCompletedChecker(t *testing.T) {
	fs := vfstest.FromMap(map[string]string{
		"/src/first.ts":   "export const first: number = 1;",
		"/src/blocked.ts": "export const blocked: number = 2;",
	}, tspath.CaseSensitive)
	released := make(chan weak.Pointer[checker.Checker], 1)
	options := &core.CompilerOptions{NoLib: core.TSTrue, Declaration: core.TSTrue, OutDir: "/out"}
	p := NewProgram(ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/src/first.ts", "/src/blocked.ts"}, nil, "/src", fs.CaseSensitivity()),
		Host:           NewCompilerHost(fs, "/", nil, nil, nil),
		SingleThreaded: core.TSFalse,
		CreateCheckerPool: func(p *Program) CheckerPool {
			return &disposableEmitCheckerPool{program: p, released: released}
		},
	})
	p.BindSourceFiles()

	blocked := make(chan struct{})
	unblock := make(chan struct{})
	finished := make(chan *EmitResult, 1)
	go func() {
		finished <- p.Emit(context.Background(), EmitOptions{
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
	}()
	t.Cleanup(func() {
		close(unblock)
		result := <-finished
		if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 {
			t.Errorf("unexpected emit result: %+v", result)
		}
	})

	<-blocked
	reference := <-released
	// The first worker has released its checker, while the second keeps Emit active.
	// Collecting results must not keep the completed worker's emit host alive.
	for range 20 {
		runtime.Gosched()
		runtime.GC()
		if reference.Value() == nil {
			return
		}
	}
	t.Fatal("completed emit worker retained its checker until other files finished")
}
