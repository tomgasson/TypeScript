package compiler

import (
	"context"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestEmitWithoutOutputSkipsChecker(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		file    tspath.RootedFilePath
		text    string
		options core.CompilerOptions
		emit    EmitOnly
	}{
		{
			name: "declarations disabled",
			file: "/src/index.ts",
			text: "export const value: number = 1;",
			emit: EmitOnlyDts,
		},
		{
			name:    "JavaScript disabled",
			file:    "/src/index.ts",
			text:    "export const value: number = 1;",
			options: core.CompilerOptions{Declaration: core.TSTrue, EmitDeclarationOnly: core.TSTrue},
			emit:    EmitOnlyJs,
		},
		{
			name:    "JSON declarations",
			file:    "/src/data.json",
			text:    `{"value":1}`,
			options: core.CompilerOptions{ResolveJsonModule: core.TSTrue, Declaration: core.TSTrue},
			emit:    EmitOnlyDts,
		},
	} {
		for _, selected := range []bool{false, true} {
			name := c.name + "/all files"
			if selected {
				name = c.name + "/selected file"
			}
			t.Run(name, func(t *testing.T) {
				fs := vfstest.FromMap(map[string]string{c.file.AsString(): c.text}, tspath.CaseSensitive)
				options := c.options
				options.NoLib = core.TSTrue
				options.OutDir = "/out"
				p := NewProgram(ProgramOptions{
					Config: tsoptions.NewParsedCommandLine(&options, []tspath.RootedFilePath{c.file}, nil, "/src", fs.CaseSensitivity()),
					Host:   NewCompilerHost(fs, "/", nil, nil, nil),
				})
				file := p.GetSourceFile(c.file)
				assert.Assert(t, file != nil)
				var targets []*ast.SourceFile
				if selected {
					targets = []*ast.SourceFile{file}
				}
				for range 2 {
					result := p.Emit(context.Background(), EmitOptions{
						TargetSourceFiles: targets,
						EmitOnly:          c.emit,
						WriteFile: func(tspath.RootedFilePath, string, *WriteFileData) error {
							t.Error("an emit request without output must not write files")
							return nil
						},
					})
					assert.Assert(t, result != nil)
					assert.Equal(t, result.EmitSkipped, false)
					assert.Equal(t, len(result.Diagnostics), 0)
					assert.Equal(t, len(result.EmittedFiles), 0)
					assert.Equal(t, len(result.SourceMaps), 0)
				}
				for _, checker := range p.compilerCheckerPool.checkers {
					assert.Assert(t, checker == nil, "an emit request without output must not initialize checkers")
				}
			})
		}
	}
}

func TestEmitPreflightPreservesForcedOutput(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		options core.CompilerOptions
		emit    EmitOnly
		force   bool
		path    tspath.RootedFilePath
		text    string
	}{
		{
			name:  "forced declaration",
			emit:  EmitOnlyDts,
			force: true,
			path:  "/out/index.d.ts",
			text:  "export declare const value: number;",
		},
		{
			name: "builder signature",
			emit: EmitOnlyBuilderSignature,
			path: "/out/index.d.ts",
			text: "export declare const value: number;",
		},
		{
			name:    "forced JavaScript",
			options: core.CompilerOptions{Declaration: core.TSTrue, EmitDeclarationOnly: core.TSTrue},
			emit:    EmitOnlyJs,
			force:   true,
			path:    "/out/index.js",
			text:    "export const value = 1;",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs := vfstest.FromMap(map[string]string{
				"/src/index.ts": "export const value: number = 1;",
			}, tspath.CaseSensitive)
			options := c.options
			options.NoLib = core.TSTrue
			options.NoEmit = core.TSTrue
			options.OutDir = "/out"
			options.Target = core.ScriptTargetES2015
			options.Module = core.ModuleKindESNext
			p := NewProgram(ProgramOptions{
				Config: tsoptions.NewParsedCommandLine(&options, []tspath.RootedFilePath{"/src/index.ts"}, nil, "/src", fs.CaseSensitivity()),
				Host:   NewCompilerHost(fs, "/", nil, nil, nil),
			})
			var written []tspath.RootedFilePath
			result := p.Emit(context.Background(), EmitOptions{
				TargetSourceFiles: []*ast.SourceFile{p.GetSourceFile("/src/index.ts")},
				EmitOnly:          c.emit,
				ForceEmit:         c.force,
				WriteFile: func(path tspath.RootedFilePath, text string, _ *WriteFileData) error {
					written = append(written, path)
					assert.Equal(t, path, c.path)
					assert.Assert(t, strings.Contains(text, c.text))
					return nil
				},
			})
			assert.Assert(t, result != nil)
			assert.Equal(t, result.EmitSkipped, false)
			assert.Equal(t, len(result.Diagnostics), 0)
			assert.DeepEqual(t, written, []tspath.RootedFilePath{c.path})
			assert.DeepEqual(t, result.EmittedFiles, written)
		})
	}
}

func TestEmitPreflightPreservesNoEmitOnError(t *testing.T) {
	t.Parallel()
	fs := vfstest.FromMap(map[string]string{
		"/src/index.ts": "export const value: number = 'wrong';",
	}, tspath.CaseSensitive)
	options := &core.CompilerOptions{NoLib: core.TSTrue, NoEmitOnError: core.TSTrue, OutDir: "/out"}
	p := NewProgram(ProgramOptions{
		Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/src/index.ts"}, nil, "/src", fs.CaseSensitivity()),
		Host:   NewCompilerHost(fs, "/", nil, nil, nil),
	})
	result := p.Emit(context.Background(), EmitOptions{
		EmitOnly: EmitOnlyDts,
		WriteFile: func(tspath.RootedFilePath, string, *WriteFileData) error {
			t.Error("noEmitOnError must prevent output")
			return nil
		},
	})
	assert.Assert(t, result != nil)
	assert.Equal(t, result.EmitSkipped, true)
	assert.Assert(t, len(result.Diagnostics) > 0, "noEmitOnError must still check before output preflight")
	assert.Equal(t, len(result.EmittedFiles), 0)
}
