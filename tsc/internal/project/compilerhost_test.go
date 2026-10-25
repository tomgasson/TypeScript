package project

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"github.com/zeebo/xxh3"
	"gotest.tools/v3/assert"
)

type suppliedTextCompilerHost struct {
	compiler.CompilerHost
	parseHost *compilerHost
	text      string
}

type suppliedTextFS struct {
	vfs.FS
	fileName tspath.RootedFilePath
}

func (fs *suppliedTextFS) FileExists(fileName tspath.RootedFilePath) bool {
	return fileName == fs.fileName || fs.FS.FileExists(fileName)
}

func (h *suppliedTextCompilerHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	return h.parseHost.GetSourceFileFromText(opts, h.text, core.ScriptKindUnknown)
}

func TestCompilerHostSuppliedTextSnapshotOwnership(t *testing.T) {
	t.Parallel()
	const fileName tspath.RootedFilePath = "/types/index.d.ts"
	const text = "export declare const value: number;"
	fs := vfstest.FromMap(map[string]string{}, tspath.CaseSensitive)
	cache := NewParseCache(RefCountCacheOptions{})
	parseHost := &compilerHost{
		project: &Project{},
		builder: &ProjectCollectionBuilder{parseCache: cache},
	}
	host := &suppliedTextCompilerHost{
		CompilerHost: compiler.NewCompilerHost(&suppliedTextFS{FS: fs, fileName: fileName}, "/", nil, nil, nil),
		parseHost:    parseHost,
		text:         text,
	}
	config := tsoptions.NewParsedCommandLine(&core.CompilerOptions{NoLib: core.TSTrue}, []tspath.RootedFilePath{fileName}, nil, "/", tspath.CaseSensitive)
	store := &SnapshotHost{parseCache: cache, programCounter: &programCounter{}}
	newSnapshot := func() *Snapshot {
		program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
		store.programCounter.Ref(program)
		snapshot := &Snapshot{
			host:               store,
			ProjectCollection:  &ProjectCollection{inferredProject: &Project{Program: program}},
			ConfigFileRegistry: &ConfigFileRegistry{},
		}
		snapshot.refCount.Store(1)
		return snapshot
	}
	first := newSnapshot()
	file := first.ProjectCollection.InferredProject().Program.GetSourceFile(fileName)
	assert.Assert(t, file != nil)
	assert.Assert(t, file.IsBound())
	assert.Equal(t, file.Hash, xxh3.HashString128(text))
	key := parseCacheKeyForFile(file)
	assert.Assert(t, cache.Has(key))

	second := newSnapshot()
	assert.Equal(t, second.ProjectCollection.InferredProject().Program.GetSourceFile(fileName), file)
	first.Deref()
	assert.Assert(t, cache.Has(key), "the second program still owns the supplied declaration")
	second.Deref()
	assert.Assert(t, !cache.Has(key), "snapshot disposal balances supplied-text acquisitions")
	assert.Equal(t, store.programCounter.Len(), 0)
	assert.Assert(t, !fs.FileExists(fileName))
}

func TestCompilerHostSuppliedTextCacheIdentity(t *testing.T) {
	t.Parallel()
	cache := NewParseCache(RefCountCacheOptions{})
	host := &compilerHost{project: &Project{}, builder: &ProjectCollectionBuilder{parseCache: cache}}
	opts := ast.SourceFileParseOptions{FileName: "/component.ts", PathKey: "/component.ts"}
	const text = "export const view = <div />;"
	file := host.GetSourceFileFromText(opts, text, core.ScriptKindTSX)
	assert.Equal(t, file.ScriptKind, core.ScriptKindTSX)
	assert.Equal(t, len(file.Diagnostics()), 0)
	key := parseCacheKeyForFile(file)
	cache.Ref(key) // The same operation used when a program clone retains this file.
	cache.Deref(key)
	assert.Assert(t, cache.Has(key))

	lease := (&SnapshotHost{parseCache: cache}).AcquireSourceFile(opts, text, core.ScriptKindTSX)
	assert.Equal(t, lease.SourceFile(), file, "host parsing and explicit leases share cache identity")
	lease.Release()
	assert.Assert(t, cache.Has(key))
	for _, changed := range []*ast.SourceFile{
		host.GetSourceFileFromText(opts, text+"\n", core.ScriptKindTSX),
		host.GetSourceFileFromText(opts, text, core.ScriptKindTS),
	} {
		assert.Assert(t, changed != file)
		cache.Deref(parseCacheKeyForFile(changed))
	}
	cache.Deref(key)
	assert.Assert(t, !cache.Has(key))
}
