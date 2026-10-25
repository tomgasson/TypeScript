package project_test

import (
	"context"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/project"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/projecttestutil"
	"gotest.tools/v3/assert"
)

func TestSameFileNamesRebuildUpdatesResolutionWatchers(t *testing.T) {
	t.Parallel()
	const uri lsproto.DocumentUri = "file:///workspace/index.ts"
	const source = "export { value } from '../external/old/dep';\n"
	session, utils := projecttestutil.SetupWithOptions(map[string]any{
		"/workspace/tsconfig.json": `{"compilerOptions":{"noLib":true,"module":"esnext","moduleResolution":"bundler"},"files":["index.ts"]}`,
		"/workspace/index.ts":      source,
		"/external/old/marker.txt": "",
		"/external/new/marker.txt": "",
	}, &project.SessionOptions{
		CurrentDirectory: "/workspace", DefaultLibraryPath: "/lib",
		PositionEncoding: lsproto.PositionEncodingKindUTF8, WatchEnabled: true,
	})
	defer session.Close()
	preferences := session.Config()
	preferences.IncludeCompletionsForModuleExports = core.TSFalse
	session.InitializeWithUserConfig(preferences)
	ctx := context.Background()
	check := func() *project.Project {
		t.Helper()
		service, err := session.GetLanguageService(ctx, uri)
		assert.NilError(t, err)
		program := service.GetProgram()
		diagnostics := program.GetSemanticDiagnostics(ctx, program.GetSourceFile("/workspace/index.ts"))
		assert.Equal(t, len(diagnostics), 1)
		assert.Equal(t, diagnostics[0].Code(), int32(2307))
		session.WaitForBackgroundTasks()
		return session.Snapshot().GetDefaultProject(uri)
	}
	session.DidOpenFile(ctx, uri, 1, source, lsproto.LanguageKindTypeScript)
	check()
	assert.Assert(t, utils.WatchesFile("/external/old/dep.ts"))
	assert.Assert(t, !utils.WatchesFile("/external/new/dep.ts"))

	// Both versions have only the consumer source file, but import resolution
	// now probes a different existing directory outside the workspace.
	session.DidChangeFile(ctx, uri, 2, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{
		WholeDocument: &lsproto.TextDocumentContentChangeWholeDocument{Text: "export { value } from '../external/new/dep';\n"},
	}})
	updated := check()
	assert.Equal(t, updated.ProgramUpdateKind, project.ProgramUpdateKindSameFileNames)
	assert.Assert(t, utils.WatchesFile("/external/new/dep.ts"), "new resolution candidates must be covered by actual client watcher registrations")
}
