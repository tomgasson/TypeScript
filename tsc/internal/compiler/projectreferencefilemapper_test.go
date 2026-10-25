package compiler

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"gotest.tools/v3/assert"
)

func TestProjectReferenceRedirectProvenance(t *testing.T) {
	t.Parallel()
	consumer := tsoptions.NewParsedCommandLine(&core.CompilerOptions{IsolatedDeclarations: core.TSTrue}, nil, nil, "/consumer", tspath.CaseSensitive)
	producer := tsoptions.NewParsedCommandLine(&core.CompilerOptions{IsolatedDeclarations: core.TSFalse}, nil, nil, "/producer", tspath.CaseSensitive)
	reference := &tsoptions.SourceOutputAndProjectReference{
		Source:        "/producer/index.ts",
		SourcePath:    "/producer/index.ts",
		OutputDts:     "/producer/dist/index.d.ts",
		OutputDtsPath: "/producer/dist/index.d.ts",
		Resolved:      producer,
	}
	mapper := &projectReferenceFileMapper{
		config:                      consumer,
		useSourceOfProjectReference: true,
		outputDtsToProjectReference: map[tspath.PathKey]*tsoptions.SourceOutputAndProjectReference{
			reference.OutputDtsPath: reference,
		},
	}
	const alias tspath.PathKey = "/consumer/node_modules/producer/dist/index.d.ts"
	mapper.realpathDtsToSource.Store(alias, reference)
	// Direct output provenance takes precedence over a cached realpath entry.
	mapper.realpathDtsToSource.Store(reference.OutputDtsPath, &tsoptions.SourceOutputAndProjectReference{
		Source:     "/other/index.ts",
		SourcePath: "/other/index.ts",
		Resolved:   consumer,
	})
	for _, path := range []tspath.PathKey{reference.OutputDtsPath, alias} {
		file := ast.NewHasFileName(tspath.RootedFilePath(path), path)
		config, source := mapper.getRedirectForResolution(file)
		assert.Equal(t, config, producer)
		assert.Equal(t, source, reference.Source)
		assert.Equal(t, mapper.getCompilerOptionsForFile(file), producer.CompilerOptions())
		redirect, redirectPath := mapper.getParseFileRedirect(file)
		assert.Equal(t, redirect, reference.Source)
		assert.Equal(t, redirectPath, reference.SourcePath)
	}
	// The direct-only accessor keeps its existing contract for checker callers.
	assert.Assert(t, mapper.getProjectReferenceFromOutputDts(alias) == nil)
	missing := ast.NewHasFileName("/unrelated/index.d.ts", "/unrelated/index.d.ts")
	config, source := mapper.getRedirectForResolution(missing)
	assert.Assert(t, config == nil)
	assert.Equal(t, source, missing.FileName())
	redirect, redirectPath := mapper.getParseFileRedirect(missing)
	assert.Equal(t, redirect, tspath.RootedFilePath(""))
	assert.Equal(t, redirectPath, tspath.PathKey(""))
}
