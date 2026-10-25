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

func TestProjectReferenceConfigChangeInvalidatesDiagnostics(t *testing.T) {
	t.Parallel()
	for _, disk := range []bool{false, true} {
		for _, preserveInitially := range []bool{false, true} {
			for _, flag := range []string{"isolatedModules", "verbatimModuleSyntax", ""} {
				t.Run(fmt.Sprintf("disk=%t/preserveInitially=%t/flag=%s", disk, preserveInitially, flag), func(t *testing.T) {
					flagOption := ""
					if flag != "" {
						flagOption = fmt.Sprintf(`,"%s":true`, flag)
					}
					newProgram := func(preserve bool, old *Program) *Program {
						files := map[string]string{
							"/globals.d.ts":           deletionTestLib,
							"/consumer/tsconfig.json": fmt.Sprintf(`{"compilerOptions":{"noLib":true,"strict":true,"incremental":true,"noEmit":true%s,"module":"esnext","moduleResolution":"bundler","tsBuildInfoFile":"./state.tsbuildinfo"},"files":["../globals.d.ts","index.ts"],"references":[{"path":"../producer"}]}`, flagOption),
							"/consumer/index.ts":      `import { Value } from '../producer/index'; const value: number = Value.One;`,
							"/producer/tsconfig.json": fmt.Sprintf(`{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"preserveConstEnums":%t,"module":"esnext","moduleResolution":"bundler","outDir":"./dist"},"files":["../globals.d.ts","index.ts"]}`, preserve),
							"/producer/index.ts":      "export const enum Value { One = 1 }",
						}
						if disk {
							files["/producer/dist/index.d.ts"] = "export declare const enum Value { One = 1 }"
						}
						fs := vfstest.FromMap(files, tspath.CaseSensitive)
						host := compiler.NewCompilerHost(fs, "/", nil, nil, nil)
						config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile("/consumer/tsconfig.json", nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
						assert.Equal(t, len(diagnostics), 0)
						assert.Assert(t, config != nil)
						assert.Equal(t, len(config.Errors), 0)
						program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
						assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") != nil)
						assert.Equal(t, fs.FileExists("/producer/dist/index.d.ts"), disk)
						return NewProgram(program, old, CreateHost(host), nil, false)
					}
					old := newProgram(preserveInitially, nil)
					oldDiagnostics := old.GetSemanticDiagnostics(t.Context(), nil)
					assert.Equal(t, len(oldDiagnostics) != 0, !preserveInitially && flag != "")
					for _, diagnostic := range oldDiagnostics {
						assert.Equal(t, diagnostic.Code(), int32(2748))
					}
					// Complete the normal check/emit cycle before reusing incremental state.
					old.Emit(t.Context(), compiler.EmitOptions{})
					oldHost := old.program.Host()
					persisted := ReadBuildInfoProgram(old.program.CommandLine(), NewBuildInfoReader(oldHost), oldHost)
					assert.Assert(t, persisted != nil)
					for _, state := range []struct {
						name    string
						program *Program
					}{{"memory", old}, {"build info", persisted}} {
						t.Run(state.name, func(t *testing.T) {
							unchanged := newProgram(preserveInitially, state.program)
							assert.Assert(t, unchanged.snapshot.changedFilesSet.IsEmpty(), "unchanged producer context must preserve reuse")
							_, reused := unchanged.snapshot.semanticDiagnosticsPerFile.Load("/consumer/index.ts")
							assert.Assert(t, reused, "unchanged consumer diagnostics should already be cached")
							assert.Equal(t, len(unchanged.GetSemanticDiagnostics(t.Context(), nil)), len(oldDiagnostics))
							current := newProgram(!preserveInitially, state.program)
							fresh := newProgram(!preserveInitially, nil)
							text := current.program.GetSourceFile("/producer/dist/index.d.ts").Text()
							assert.Equal(t, text, old.program.GetSourceFile("/producer/dist/index.d.ts").Text(), "only producer configuration changed")
							info, ok := current.snapshot.fileInfos.Load("/producer/dist/index.d.ts")
							assert.Assert(t, ok)
							assert.Equal(t, info.version == ComputeHash(text, false), flag == "", "context tagging is limited to consumers that enforce the const-enum restriction")
							expected := fresh.GetSemanticDiagnostics(t.Context(), nil)
							assert.Equal(t, len(expected) != 0, preserveInitially && flag != "")
							for _, diagnostic := range expected {
								assert.Equal(t, diagnostic.Code(), int32(2748))
							}
							actual := current.GetSemanticDiagnostics(t.Context(), nil)
							assert.Equal(t, len(actual), len(expected), "incremental diagnostics must reflect changed producer configuration: incremental=%v fresh=%v", actual, expected)
							for i, diagnostic := range actual {
								assert.Equal(t, diagnostic.Code(), expected[i].Code())
							}
						})
					}
				})
			}
		}
	}
}
