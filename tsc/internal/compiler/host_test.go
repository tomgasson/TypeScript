package compiler

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestCompilerHostGetSourceFileFromText(t *testing.T) {
	t.Parallel()
	fs := vfstest.FromMap(map[string]string{}, tspath.CaseSensitive)
	host := NewCompilerHost(fs, "/", nil, nil, nil)
	opts := ast.SourceFileParseOptions{FileName: "/types/index.d.ts", PathKey: "/types/index.d.ts"}
	const text = "export declare const value: number;"
	file := host.GetSourceFileFromText(opts, text, core.ScriptKindUnknown)
	assert.Equal(t, file.Text(), text)
	assert.Equal(t, file.ParseOptions(), opts)
	assert.Equal(t, file.ScriptKind, core.ScriptKindTS)
	assert.Assert(t, file.IsDeclarationFile)
	assert.Assert(t, !fs.FileExists(opts.FileName))
	assert.Assert(t, host.GetSourceFile(opts) == nil)

	jsx := host.GetSourceFileFromText(ast.SourceFileParseOptions{
		FileName: "/component.ts", PathKey: "/component.ts",
	}, "export const view = <div />;", core.ScriptKindTSX)
	assert.Equal(t, jsx.ScriptKind, core.ScriptKindTSX)
	assert.Equal(t, len(jsx.Diagnostics()), 0)
}
