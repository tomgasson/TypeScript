package compiler

import (
	"context"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestDeclarationDiagnosticsSkipChecker(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		file    tspath.RootedFilePath
		files   map[string]string
		options *core.CompilerOptions
	}{
		{
			name: "declaration",
			file: "/src/index.d.ts",
			files: map[string]string{
				"/src/index.d.ts": "export declare const value: number;",
			},
			options: &core.CompilerOptions{},
		},
		{
			name: "json",
			file: "/src/data.json",
			files: map[string]string{
				"/src/data.json": `{"value":1}`,
			},
			options: &core.CompilerOptions{ResolveJsonModule: core.TSTrue, OutDir: "/out"},
		},
		{
			name: "excluded JavaScript",
			file: "/src/index.js",
			files: map[string]string{
				"/src/index.js": "export const value = 1;",
			},
			options: &core.CompilerOptions{AllowJs: core.TSTrue, NoEmitForJsFiles: core.TSTrue},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs := vfstest.FromMap(c.files, tspath.CaseSensitive)
			c.options.NoLib = core.TSTrue
			c.options.Declaration = core.TSTrue
			p := NewProgram(ProgramOptions{
				Config: tsoptions.NewParsedCommandLine(c.options, []tspath.RootedFilePath{c.file}, nil, "/src", fs.CaseSensitivity()),
				Host:   NewCompilerHost(fs, "/", nil, nil, nil),
			})
			file := p.GetSourceFile(c.file)
			assert.Assert(t, file != nil)
			for range 2 {
				assert.Equal(t, len(p.GetDeclarationDiagnostics(context.Background(), file)), 0)
			}
			for _, checker := range p.compilerCheckerPool.checkers {
				assert.Assert(t, checker == nil, "a skipped diagnostic request must not initialize checkers")
			}
		})
	}
}

func TestDeclarationDiagnosticsReferencedSource(t *testing.T) {
	t.Parallel()
	fs := vfstest.FromMap(map[string]string{
		"/src/tsconfig.json": `{"compilerOptions":{"noLib":true,"declaration":true,"isolatedDeclarations":true},"files":["index.ts"],"references":[{"path":"../dep"}]}`,
		"/src/index.ts":      "export { value } from '../dep/index';",
		"/dep/tsconfig.json": `{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true},"files":["index.ts"]}`,
		"/dep/index.ts":      "export function value() { return 1; }",
	}, tspath.CaseSensitive)
	config, diags := tsoptions.GetParsedCommandLineOfConfigFile("/src/tsconfig.json", nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
	assert.Equal(t, len(diags), 0)
	p := NewProgram(ProgramOptions{
		Config:                      config,
		Host:                        NewCompilerHost(fs, "/", nil, nil, nil),
		UseSourceOfProjectReference: true,
	})
	file := p.GetSourceFile("/dep/index.ts")
	assert.Assert(t, file != nil)
	assert.Equal(t, len(p.GetDeclarationDiagnostics(context.Background(), file)), 0)
	for _, checker := range p.compilerCheckerPool.checkers {
		assert.Assert(t, checker == nil, "the consuming program does not report declaration errors in referenced sources")
	}
}

func TestDeclarationDiagnosticsStillChecksImplementation(t *testing.T) {
	t.Parallel()
	fs := vfstest.FromMap(map[string]string{
		"/src/index.ts": "function internal(): number { return 1; } export function value() { return internal(); }",
	}, tspath.CaseSensitive)
	options := &core.CompilerOptions{NoLib: core.TSTrue, Declaration: core.TSTrue, IsolatedDeclarations: core.TSTrue}
	p := NewProgram(ProgramOptions{
		Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/src/index.ts"}, nil, "/src", fs.CaseSensitivity()),
		Host:   NewCompilerHost(fs, "/", nil, nil, nil),
	})
	file := p.GetSourceFile("/src/index.ts")
	assert.Assert(t, file != nil)
	for range 2 {
		diags := p.GetDeclarationDiagnostics(context.Background(), file)
		assert.Equal(t, len(diags), 1)
		assert.Equal(t, diags[0].Code(), int32(9013))
		assert.Equal(t, diags[0].File(), file)
	}
}
