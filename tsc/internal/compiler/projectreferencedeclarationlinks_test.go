package compiler

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestProjectReferenceDeclarationsReadOnlySymlink(t *testing.T) {
	t.Parallel()
	for _, mapFirst := range []bool{false, true} {
		name := core.IfElse(mapFirst, "map first", "declaration first")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const source tspath.RootedFilePath = "/producer/src/index.ts"
			const output tspath.RootedFilePath = "/producer/dist/index.d.ts"
			const alias tspath.RootedFilePath = "/consumer/node_modules/pkg/dist/index.d.ts"
			const declaration = "export declare const value: number;\n//# sourceMappingURL=index.d.ts.map\n"
			const declarationMap = `{"version":3,"file":"index.d.ts","sources":["../src/index.ts"],"names":[],"mappings":"AAAA"}`
			physical := vfstest.FromMap(map[string]any{
				source.AsString():            "export const value: number = 1;",
				"/producer/package.json":     `{"name":"pkg","types":"dist/index.d.ts"}`,
				"/consumer/node_modules/pkg": vfstest.Symlink("/producer"),
			}, tspath.CaseSensitive)
			config := tsoptions.NewParsedCommandLine(&core.CompilerOptions{
				Composite: core.TSTrue, IsolatedDeclarations: core.TSTrue, PreserveSymlinks: core.TSTrue,
			}, []tspath.RootedFilePath{source}, nil, "/producer", tspath.CaseSensitive)
			reference := &tsoptions.SourceOutputAndProjectReference{
				Source: source, SourcePath: tspath.PathKey(source),
				OutputDts: output, OutputDtsPath: tspath.PathKey(output), Resolved: config,
			}
			mapper := &projectReferenceFileMapper{
				config: config,
				outputDtsToProjectReference: map[tspath.PathKey]*tsoptions.SourceOutputAndProjectReference{
					tspath.PathKey(output): reference,
				},
			}
			mapper.declarationProjects.Add(config)
			mapper.dtsDirectories.Add("/producer/dist")
			mapper.generatedDeclarations.Store(tspath.PathKey(output), declaration)
			mapper.generatedDeclarations.Store(tspath.PathKey(output.AppendSuffix(".map")), declarationMap)
			assert.Assert(t, !physical.DirectoryExists("/producer/dist"))
			assert.Assert(t, !physical.FileExists(output))
			assert.Assert(t, !physical.FileExists(alias))

			// A later resolver has no generation callback and has not encountered
			// this alias yet. Preserve-symlink callers must be able to read the
			// declaration and its map without first canonicalizing their names.
			host := newProjectReferenceDtsFakingHost(&compilerResolutionHost{
				host: NewCompilerHost(physical, "/", nil, nil, nil), baseDirectory: "/consumer",
			}, mapper, nil)
			virtual := host.FS()
			paths := []tspath.RootedFilePath{alias, alias.AppendSuffix(".map")}
			if mapFirst {
				paths[0], paths[1] = paths[1], paths[0]
			}
			for _, path := range paths {
				assert.Assert(t, virtual.FileExists(path), "generated alias must exist: %s", path)
				text, ok := virtual.ReadFile(path)
				assert.Assert(t, ok, "generated alias must be readable: %s", path)
				assert.Equal(t, text, core.IfElse(path == alias, declaration, declarationMap))
			}
			assert.Equal(t, virtual.Realpath(alias.AsPath()), output.AsPath())
			assert.Assert(t, !virtual.FileExists("/consumer/node_modules/pkg/dist/missing.d.ts"))
			assert.Assert(t, !physical.FileExists(output), "reads must not materialize generated output")
			assert.Assert(t, !physical.FileExists(output.AppendSuffix(".map")))
		})
	}
}
