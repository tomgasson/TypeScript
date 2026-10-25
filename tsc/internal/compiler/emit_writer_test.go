package compiler

import (
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestSerialAndParallelEmitWriters(t *testing.T) {
	t.Parallel()
	for _, declarationOnly := range []bool{false, true} {
		var expected map[tspath.RootedFilePath]string
		for _, singleThreaded := range []core.Tristate{core.TSFalse, core.TSTrue} {
			fs := vfstest.FromMap(map[string]string{
				"/src/a.ts": "export const a: number = 1;\n",
				"/src/b.ts": "/** " + strings.Repeat("long comment ", 128) + " */\nexport function b(value: string): string { return value; }\n",
				"/src/c.ts": "export interface C { value: boolean; }\nexport const c: C = { value: true };\n",
			}, tspath.CaseSensitive)
			options := &core.CompilerOptions{
				NoLib: core.TSTrue, Declaration: core.TSTrue, DeclarationMap: core.TSTrue, SourceMap: core.TSTrue,
				IsolatedDeclarations: core.TSTrue, Module: core.ModuleKindESNext, OutDir: "/out", RootDir: "/src",
			}
			program := NewProgram(ProgramOptions{
				Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/src/a.ts", "/src/b.ts", "/src/c.ts"}, nil, "/src", fs.CaseSensitivity()),
				Host:   NewCompilerHost(fs, "/", nil, nil, nil), SingleThreaded: singleThreaded,
			})
			for range 2 {
				outputs := make(map[tspath.RootedFilePath]string)
				var mu sync.Mutex
				result := program.Emit(t.Context(), EmitOptions{
					EmitOnly: core.IfElse(declarationOnly, EmitOnlyDts, EmitAll),
					WriteFile: func(path tspath.RootedFilePath, text string, _ *WriteFileData) error {
						mu.Lock()
						defer mu.Unlock()
						outputs[path] = text
						return nil
					},
				})
				assert.Assert(t, !result.EmitSkipped)
				assert.Equal(t, len(result.Diagnostics), 0)
				assert.Equal(t, len(outputs), core.IfElse(declarationOnly, 6, 12))
				if expected == nil {
					expected = outputs
				} else {
					assert.Assert(t, maps.Equal(outputs, expected), "writer reuse must preserve every output and source map, including earlier captured strings")
				}
			}
		}
	}
}
