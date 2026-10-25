package incremental

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestProjectReferenceDeclarationSourceModuleChange(t *testing.T) {
	t.Parallel()
	for _, initiallyESM := range []bool{false, true} {
		t.Run(fmt.Sprintf("initiallyESM=%t", initiallyESM), func(t *testing.T) {
			create := func(esm bool, old *Program) *Program {
				module := core.IfElse(esm, "esnext", "commonjs")
				fs := vfstest.FromMap(map[string]string{
					"/globals.d.ts":           deletionTestLib,
					"/consumer/tsconfig.json": `{"compilerOptions":{"noLib":true,"strict":true,"incremental":true,"noEmit":true,"module":"esnext","moduleResolution":"bundler","tsBuildInfoFile":"./state.tsbuildinfo"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../producer"}]}`,
					"/consumer/index.ts":      `import value from '../producer/index'; const result: number = value.named;`,
					"/producer/tsconfig.json": fmt.Sprintf(`{"compilerOptions":{"noLib":true,"composite":true,"module":%q,"moduleResolution":"bundler"},"files":["index.d.ts"]}`, module),
					"/producer/index.d.ts":    "export declare const named: number;",
				}, tspath.CaseSensitive)
				host := compiler.NewCompilerHost(fs, "/", nil, nil, nil)
				config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile("/consumer/tsconfig.json", nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
				assert.Equal(t, len(diagnostics), 0)
				program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
				file := program.GetSourceFile("/producer/index.d.ts")
				assert.Assert(t, file != nil)
				assert.Assert(t, program.GetRedirectForResolution(file) != nil, "a handwritten declaration uses the producer source mapping")
				assert.Assert(t, program.GetProjectReferenceFromOutputDts(file.PathKey()) == nil, "a handwritten declaration has no emitted output mapping")
				return NewProgram(program, old, CreateHost(host), nil, false)
			}
			old := create(initiallyESM, nil)
			assert.Equal(t, len(old.GetSemanticDiagnostics(t.Context(), nil)), core.IfElse(initiallyESM, 1, 0))
			old.Emit(t.Context(), compiler.EmitOptions{})
			persisted := ReadBuildInfoProgram(old.program.CommandLine(), NewBuildInfoReader(old.program.Host()), old.program.Host())
			assert.Assert(t, persisted != nil)
			for _, state := range []struct {
				name    string
				program *Program
			}{{"memory", old}, {"build info", persisted}} {
				t.Run(state.name, func(t *testing.T) {
					unchanged := create(initiallyESM, state.program)
					assert.Assert(t, unchanged.snapshot.changedFilesSet.IsEmpty())
					changed := create(!initiallyESM, state.program)
					fresh := create(!initiallyESM, nil)
					cold := fresh.GetSemanticDiagnostics(t.Context(), nil)
					assert.Equal(t, len(cold), core.IfElse(initiallyESM, 0, 1))
					for _, diagnostic := range cold {
						assert.Equal(t, diagnostic.Code(), int32(1192))
					}
					got := changed.GetSemanticDiagnostics(t.Context(), nil)
					assert.Equal(t, len(got), len(cold), "referenced handwritten declarations must preserve producer module semantics")
					for i, diagnostic := range got {
						assert.Equal(t, diagnostic.Code(), cold[i].Code())
					}
				})
			}
		})
	}
}
