package compiler

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"weak"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type projectReferenceEmissionOwnership struct {
	outputs  map[tspath.RootedFilePath]string
	program  weak.Pointer[Program]
	sources  []weak.Pointer[ast.SourceFile]
	checkers []weak.Pointer[checker.Checker]
}

//go:noinline
func captureProjectReferenceEmissionOwnership(t *testing.T, invalid bool) projectReferenceEmissionOwnership {
	t.Helper()
	second := "export const total: number = 1;"
	if invalid {
		second = "function internal(): number { return 1; } export function total() { return internal(); }"
	}
	fs := vfstest.FromMap(map[string]string{
		"/producer/src/a.ts": "export function read(value: number): number { const implementation = 'body'; return value + implementation.length; }",
		"/producer/src/b.ts": second,
	}, tspath.CaseSensitive)
	options := &core.CompilerOptions{
		NoLib:                core.TSTrue,
		Declaration:          core.TSTrue,
		DeclarationMap:       core.TSTrue,
		IsolatedDeclarations: core.TSTrue,
		RootDir:              "/producer/src",
		OutDir:               "/producer/types",
	}
	program := NewProgram(ProgramOptions{
		Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{
			"/producer/src/a.ts", "/producer/src/b.ts",
		}, nil, "/producer", fs.CaseSensitivity()),
		Host:           NewCompilerHost(fs, "/", nil, nil, nil),
		SingleThreaded: core.TSTrue,
	})
	ownership := projectReferenceEmissionOwnership{
		program: weak.Make(program),
		outputs: emitProjectReferenceDeclarations(program),
	}
	for _, file := range program.SourceFiles() {
		ownership.sources = append(ownership.sources, weak.Make(file))
	}
	for _, c := range program.compilerCheckerPool.checkers {
		if c != nil {
			ownership.checkers = append(ownership.checkers, weak.Make(c))
		}
	}
	if len(ownership.sources) != 2 || len(ownership.checkers) == 0 {
		t.Fatal("fixture did not load and emit both producer files")
	}
	return ownership
}

func (o projectReferenceEmissionOwnership) collected() bool {
	if o.program.Value() != nil {
		return false
	}
	for _, file := range o.sources {
		if file.Value() != nil {
			return false
		}
	}
	for _, c := range o.checkers {
		if c.Value() != nil {
			return false
		}
	}
	return true
}

func TestProjectReferenceDeclarationOutputReleasesProducer(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "successful emit"
		if invalid {
			name = "rejected isolated declaration"
		}
		t.Run(name, func(t *testing.T) {
			ownership := captureProjectReferenceEmissionOwnership(t, invalid)
			if invalid {
				if ownership.outputs != nil {
					t.Fatal("failed isolated declaration emit retained partial outputs")
				}
			} else if len(ownership.outputs) != 4 {
				t.Fatalf("expected two declarations and two maps, got %d outputs", len(ownership.outputs))
			}
			collected := false
			for range 20 {
				runtime.Gosched()
				runtime.GC()
				if ownership.collected() {
					collected = true
					break
				}
			}
			runtime.KeepAlive(ownership.outputs)
			if !collected {
				t.Fatal("declaration capture retained its producer program, source tree, or checker")
			}
			if invalid {
				return
			}
			// The retained signature remains usable after all producer objects die.
			text := ownership.outputs["/producer/types/a.d.ts"]
			if !strings.Contains(text, "export declare function read(value: number): number;") || strings.Contains(text, "implementation") {
				t.Fatalf("unexpected retained declaration: %s", text)
			}
			file := parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: "/producer/types/a.d.ts", PathKey: "/producer/types/a.d.ts",
			}, text, core.ScriptKindTS)
			if !file.IsDeclarationFile || len(file.Diagnostics()) != 0 {
				t.Fatal("retained declaration could not be parsed independently")
			}
			for _, name := range []tspath.RootedFilePath{"/producer/types/a.d.ts.map", "/producer/types/b.d.ts.map"} {
				if !json.Valid([]byte(ownership.outputs[name])) {
					t.Fatalf("retained declaration map is invalid: %s", name)
				}
			}
		})
	}
}
