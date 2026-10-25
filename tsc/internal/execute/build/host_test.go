package build

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestSuppliedTextDoesNotUseBuildDiskCache(t *testing.T) {
	t.Parallel()
	const diskText = "export declare const value: number;"
	const suppliedText = "export declare const value: string;"
	fs := vfstest.FromMap(map[string]string{"/index.d.ts": diskText}, tspath.CaseSensitive)
	host := &host{host: compiler.NewCompilerHost(fs, "/", nil, nil, nil)}
	opts := ast.SourceFileParseOptions{FileName: "/index.d.ts", PathKey: "/index.d.ts"}
	disk := host.GetSourceFile(opts)
	supplied := host.GetSourceFileFromText(opts, suppliedText, core.ScriptKindUnknown)
	assert.Equal(t, disk.Text(), diskText)
	assert.Equal(t, supplied.Text(), suppliedText)
	assert.Assert(t, supplied != disk)
	assert.Equal(t, host.GetSourceFile(opts), disk)
}
