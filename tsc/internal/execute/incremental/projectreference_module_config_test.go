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

func TestProjectReferenceModuleChangeInvalidatesDiagnostics(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, consumerModule, consumerResolution, consumerFile, consumerText, producerText, declaration string
		producerModules                                                                                 [2]string
	}{
		{
			name: "module format", consumerModule: "esnext", consumerResolution: "bundler", consumerFile: "index.ts",
			consumerText: `import value from '../producer/index'; const result: number = value.named;`,
			producerText: "export const named: number = 1;", declaration: "export declare const named: number;\n",
			producerModules: [2]string{"commonjs", "esnext"},
		},
		{
			name: "implied format", consumerModule: "nodenext", consumerResolution: "nodenext", consumerFile: "index.mts",
			consumerText:    `import value from '../producer/index.js'; const result: number = value.named;`,
			producerText:    "export const __esModule: boolean = true; export const named: number = 1;",
			declaration:     "export declare const __esModule: boolean;\nexport declare const named: number;\n",
			producerModules: [2]string{"nodenext", "commonjs"},
		},
	} {
		for _, disk := range []bool{false, true} {
			for _, initiallyError := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/disk=%t/initiallyError=%t", scenario.name, disk, initiallyError), func(t *testing.T) {
					newProgram := func(withError bool, old *Program) *Program {
						module := scenario.producerModules[core.IfElse(withError, 1, 0)]
						producerResolution := core.IfElse(module == "nodenext", "nodenext", "bundler")
						files := map[string]string{
							"/globals.d.ts":                      deletionTestLib,
							"/consumer/tsconfig.json":            fmt.Sprintf(`{"compilerOptions":{"noLib":true,"strict":true,"incremental":true,"noEmit":true,"module":%q,"moduleResolution":%q,"tsBuildInfoFile":"./state.tsbuildinfo"},"files":["../globals.d.ts",%q],"references":[{"path":"../producer"}]}`, scenario.consumerModule, scenario.consumerResolution, scenario.consumerFile),
							"/consumer/" + scenario.consumerFile: scenario.consumerText,
							"/producer/tsconfig.json":            fmt.Sprintf(`{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"module":%q,"moduleResolution":%q,"outDir":"./dist"},"files":["../globals.d.ts","index.ts"]}`, module, producerResolution),
							"/producer/index.ts":                 scenario.producerText,
						}
						if disk {
							files["/producer/dist/index.d.ts"] = scenario.declaration
						}
						fs := vfstest.FromMap(files, tspath.CaseSensitive)
						host := compiler.NewCompilerHost(fs, "/", nil, nil, nil)
						config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile("/consumer/tsconfig.json", nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
						assert.Equal(t, len(diagnostics), 0)
						assert.Assert(t, config != nil)
						program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
						assert.Equal(t, len(program.GetProgramDiagnostics()), 0)
						assert.Assert(t, !program.Options().GetIsolatedModules(), "module context matters even without either isolated-modules flag")
						assert.Assert(t, program.GetSourceFile("/producer/dist/index.d.ts") != nil)
						assert.Equal(t, fs.FileExists("/producer/dist/index.d.ts"), disk)
						return NewProgram(program, old, CreateHost(host), nil, false)
					}
					old := newProgram(initiallyError, nil)
					oldDiagnostics := old.GetSemanticDiagnostics(t.Context(), nil)
					assert.Equal(t, len(oldDiagnostics), core.IfElse(initiallyError, 1, 0))
					for _, diagnostic := range oldDiagnostics {
						assert.Equal(t, diagnostic.Code(), int32(1192))
					}
					old.Emit(t.Context(), compiler.EmitOptions{})
					persisted := ReadBuildInfoProgram(old.program.CommandLine(), NewBuildInfoReader(old.program.Host()), old.program.Host())
					assert.Assert(t, persisted != nil)
					for _, state := range []struct {
						name    string
						program *Program
					}{{"memory", old}, {"build info", persisted}} {
						t.Run(state.name, func(t *testing.T) {
							unchanged := newProgram(initiallyError, state.program)
							assert.Assert(t, unchanged.snapshot.changedFilesSet.IsEmpty(), "unchanged producer options must retain normal reuse")
							assert.Equal(t, len(unchanged.GetSemanticDiagnostics(t.Context(), nil)), len(oldDiagnostics))
							current := newProgram(!initiallyError, state.program)
							fresh := newProgram(!initiallyError, nil)
							const output tspath.RootedFilePath = "/producer/dist/index.d.ts"
							file := current.program.GetSourceFile(output)
							assert.Equal(t, file.Text(), old.program.GetSourceFile(output).Text())
							assert.Equal(t, current.program.GetSourceFileMetaData(file.PathKey()), old.program.GetSourceFileMetaData(file.PathKey()), "ordinary declaration metadata remains unchanged")
							if scenario.name == "implied format" {
								assert.Equal(t, current.program.GetEmitModuleFormatOfFile(file), old.program.GetEmitModuleFormatOfFile(old.program.GetSourceFile(output)), "effective module format alone cannot detect this change")
								assert.Assert(t, current.program.GetImpliedNodeFormatForEmit(file) != old.program.GetImpliedNodeFormatForEmit(old.program.GetSourceFile(output)))
							}
							info, ok := current.snapshot.fileInfos.Load(file.PathKey())
							assert.Assert(t, ok)
							oldInfo, ok := old.snapshot.fileInfos.Load(file.PathKey())
							assert.Assert(t, ok)
							assert.Equal(t, info.version, ComputeHash(file.Text(), false), "producer context must not change the content version")
							assert.Equal(t, info.version, oldInfo.version)
							assert.Equal(t, info.signature, oldInfo.signature, "propagation must still observe the previous signature")
							expected := fresh.GetSemanticDiagnostics(t.Context(), nil)
							assert.Equal(t, len(expected), core.IfElse(initiallyError, 0, 1))
							for _, diagnostic := range expected {
								assert.Equal(t, diagnostic.Code(), int32(1192))
							}
							actual := current.GetSemanticDiagnostics(t.Context(), nil)
							assert.Equal(t, len(actual), len(expected), "producer module change must invalidate synthetic-default diagnostics: incremental=%v fresh=%v", actual, expected)
							assert.Assert(t, info.signature != oldInfo.signature, "unchanged declaration text has a different semantic signature")
							freshInfo, ok := fresh.snapshot.fileInfos.Load(file.PathKey())
							assert.Assert(t, ok)
							assert.Equal(t, info.signature, freshInfo.signature, "incremental and cold signatures must converge")
							for i, diagnostic := range actual {
								assert.Equal(t, diagnostic.Code(), expected[i].Code())
							}
							current.Emit(t.Context(), compiler.EmitOptions{})
							reloaded := ReadBuildInfoProgram(current.program.CommandLine(), NewBuildInfoReader(current.program.Host()), current.program.Host())
							assert.Assert(t, reloaded != nil)
							next := newProgram(!initiallyError, reloaded)
							assert.Assert(t, next.snapshot.changedFilesSet.IsEmpty(), "updated declaration signature must survive build-info serialization")
							assert.Equal(t, len(next.GetSemanticDiagnostics(t.Context(), nil)), len(expected))
						})
					}
				})
			}
		}
	}
}
