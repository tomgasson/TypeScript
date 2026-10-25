package project

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func generatedDeclarationSession(t *testing.T, secondType string, declarationMap bool) (*Session, vfs.FS) {
	t.Helper()
	files := map[string]string{}
	types := []string{"number"}
	if secondType != "" {
		types = append(types, secondType)
	}
	for i, valueType := range types {
		name := fmt.Sprintf("%d", i)
		files["/producer"+name+"/tsconfig.json"] = fmt.Sprintf(`{"compilerOptions":{"noLib":true,"composite":true,"isolatedDeclarations":true,"declarationMap":%t,"rootDir":".","declarationDir":"../types","module":"esnext","moduleResolution":"bundler"},"files":["index.ts"]}`, declarationMap)
		value := core.IfElse(valueType == "number", "1", `"value"`)
		files["/producer"+name+"/index.ts"] = "export function value(): " + valueType + " { return " + value + "; }\n"
		files["/consumer"+name+"/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"module":"esnext","moduleResolution":"bundler","disableSourceOfProjectReferenceRedirect":true},"files":["index.ts"],"references":[{"path":"../producer` + name + `"}]}`
		files["/consumer"+name+"/index.ts"] = "import { value } from '../producer" + name + "/index';\nexport const result = value();\n"
	}
	if declarationMap {
		// The declaration is missing, but an old map remains from an earlier build.
		files["/types/index.d.ts.map"] = `{"version":3,"file":"index.d.ts","sources":["wrong.ts"],"names":[],"mappings":"AAAA"}`
	}
	fs := vfstest.FromMap(files, tspath.CaseSensitive)
	session := NewSession(&SessionInit{
		BackgroundCtx: context.Background(), FS: fs, Client: noopClient{},
		Options: &SessionOptions{CurrentDirectory: "/", DefaultLibraryPath: "/", PositionEncoding: lsproto.PositionEncodingKindUTF8},
	})
	t.Cleanup(session.Close)
	preferences := session.Config()
	preferences.IncludeCompletionsForModuleExports = core.TSFalse
	session.InitializeWithUserConfig(preferences)
	for i := range types {
		path := fmt.Sprintf("/consumer%d/index.ts", i)
		session.DidOpenFile(context.Background(), lsproto.DocumentUri("file://"+path), 1, files[path], lsproto.LanguageKindTypeScript)
	}
	session.WaitForBackgroundTasks()
	return session, fs
}

func assertGeneratedDefinition(t *testing.T, session *Session, consumer int, target lsproto.DocumentUri) {
	t.Helper()
	uri := lsproto.DocumentUri(fmt.Sprintf("file:///consumer%d/index.ts", consumer))
	service, err := session.GetLanguageService(context.Background(), uri)
	assert.NilError(t, err)
	definition, err := service.ProvideDefinition(context.Background(), uri, lsproto.Position{Line: 0, Character: 10})
	assert.NilError(t, err)
	assert.Assert(t, definition.Locations != nil)
	assert.Equal(t, len(*definition.Locations), 1)
	assert.Equal(t, (*definition.Locations)[0].Uri, target)
}

func TestGeneratedDeclarationSnapshotReads(t *testing.T) {
	t.Parallel()
	session, fs := generatedDeclarationSession(t, "", false)
	const output tspath.RootedFilePath = "/types/index.d.ts"
	snapshot := session.Snapshot()
	file := snapshot.GetFile(output)
	assert.Assert(t, file != nil)
	assert.Assert(t, strings.Contains(file.Content(), "value(): number"))
	assert.Assert(t, snapshot.FileExists(output))
	assert.Assert(t, snapshot.LSPLineMap(output) != nil)
	assert.Assert(t, snapshot.GetECMALineInfo(output) != nil)
	text, ok := snapshot.ReadFile(output)
	assert.Assert(t, ok)
	assert.Equal(t, text, file.Content())
	assert.Assert(t, !fs.FileExists(output), "virtual output must not be written to the base filesystem")
	assertGeneratedDefinition(t, session, 0, "file:///types/index.d.ts")

	const uri lsproto.DocumentUri = "file:///consumer0/index.ts"
	session.DidChangeFile(context.Background(), uri, 2, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{
		WholeDocument: &lsproto.TextDocumentContentChangeWholeDocument{Text: "import { value } from '../producer0/index';\nexport const result = value() + 1;\n"},
	}})
	assertGeneratedDefinition(t, session, 0, "file:///types/index.d.ts")
	session.WaitForBackgroundTasks()
	assert.Equal(t, session.Snapshot().GetDefaultProject(uri).ProgramUpdateKind, ProgramUpdateKindCloned)
	clonedText, ok := session.Snapshot().ReadFile(output)
	assert.Assert(t, ok)
	assert.Equal(t, clonedText, text)
}

func TestGeneratedDeclarationSnapshotConflicts(t *testing.T) {
	t.Parallel()
	for _, secondType := range []string{"number", "string"} {
		t.Run(secondType, func(t *testing.T) {
			t.Parallel()
			session, _ := generatedDeclarationSession(t, secondType, false)
			snapshot := session.Snapshot()
			const output tspath.RootedFilePath = "/types/index.d.ts"
			global := snapshot.GetFile(output)
			assert.Equal(t, global != nil, secondType == "number", "snapshot-wide reads must not choose between conflicting project outputs")
			for i, valueType := range []string{"number", secondType} {
				uri := lsproto.DocumentUri(fmt.Sprintf("file:///consumer%d/index.ts", i))
				program := snapshot.GetDefaultProject(uri).Program
				host := snapshot.LanguageServiceHost(program)
				text, ok := host.ReadFile(output)
				assert.Assert(t, ok)
				assert.Assert(t, strings.Contains(text, "value(): "+valueType))
				assert.Assert(t, host.GetECMALineInfo(output) != nil)
				assertGeneratedDefinition(t, session, i, "file:///types/index.d.ts")
				// Cross-project operations such as workspace symbols supply an
				// authoritative SourceFile even if the global path is ambiguous.
				file := program.GetSourceFile(output)
				_, fidelity := snapshot.Converters().ToLSPRange(file, core.NewTextRange(0, len(file.Text())))
				assert.Assert(t, fidelity.IsSingleSegment())
			}
		})
	}
}

func TestGeneratedDeclarationMapOverridesStaleDiskMap(t *testing.T) {
	t.Parallel()
	session, fs := generatedDeclarationSession(t, "", true)
	const mapName tspath.RootedFilePath = "/types/index.d.ts.map"
	snapshot := session.Snapshot()
	generated, ok := snapshot.ReadFile(mapName)
	assert.Assert(t, ok)
	physical, ok := fs.ReadFile(mapName)
	assert.Assert(t, ok)
	assert.Assert(t, generated != physical)
	assert.Assert(t, strings.Contains(physical, "wrong.ts"), "base filesystem map must remain untouched")
	assertGeneratedDefinition(t, session, 0, "file:///producer0/index.ts")
}

func TestGeneratedDeclarationWithoutMapIgnoresUnrelatedMap(t *testing.T) {
	t.Parallel()
	session, fs := generatedDeclarationSession(t, "", false)
	assert.NilError(t, fs.WriteFile("/types/index.d.ts.map", `{"version":3,"file":"index.d.ts","sources":["wrong.ts"],"names":[],"mappings":"AAAA"}`))
	snapshot := session.Snapshot()
	program := snapshot.GetDefaultProject("file:///consumer0/index.ts").Program
	_, ok := snapshot.LanguageServiceHost(program).ReadFile("/types/index.d.ts.map")
	assert.Assert(t, !ok, "a synthesized declaration without a map cannot use a different map")
	assertGeneratedDefinition(t, session, 0, "file:///types/index.d.ts")
}

func TestPhysicalDeclarationReplacesGeneratedTextAndMap(t *testing.T) {
	t.Parallel()
	session, fs := generatedDeclarationSession(t, "", true)
	ctx := context.Background()
	const uri lsproto.DocumentUri = "file:///consumer0/index.ts"
	const output tspath.RootedFilePath = "/types/index.d.ts"
	const mapName tspath.RootedFilePath = "/types/index.d.ts.map"
	const consumer = "import { value } from '../producer0/index';\nexport const result: number = value();\n"
	session.DidChangeFile(ctx, uri, 2, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{WholeDocument: &lsproto.TextDocumentContentChangeWholeDocument{Text: consumer}}})
	oldService, err := session.GetLanguageService(ctx, uri)
	assert.NilError(t, err)
	session.WaitForBackgroundTasks()
	oldSnapshot := session.Snapshot()
	session.RetainSnapshot(oldSnapshot)
	defer oldSnapshot.Deref()
	oldText, ok := oldService.ReadFile(output)
	assert.Assert(t, ok)
	assertGeneratedDefinition(t, session, 0, "file:///producer0/index.ts")

	const diskText = "\n\nexport declare function value(): string;\n"
	assert.NilError(t, fs.Remove(mapName.AsPath()))
	assert.NilError(t, fs.WriteFile(output, diskText))
	session.DidChangeWatchedFiles(ctx, []*lsproto.FileEvent{
		{Uri: "file:///types/index.d.ts.map", Type: lsproto.FileChangeTypeDeleted},
		{Uri: "file:///types/index.d.ts", Type: lsproto.FileChangeTypeCreated},
	})
	checkDisk := func() {
		t.Helper()
		service, err := session.GetLanguageService(ctx, uri)
		assert.NilError(t, err)
		program := service.GetProgram()
		_, generated := program.GetGeneratedDeclarationText("/types/index.d.ts")
		assert.Assert(t, !generated)
		_, generatedMap := program.GetGeneratedDeclarationText("/types/index.d.ts.map")
		assert.Assert(t, !generatedMap)
		text, ok := service.ReadFile(output)
		assert.Assert(t, ok)
		assert.Equal(t, text, diskText)
		assert.Equal(t, session.Snapshot().GetFile(output).Content(), diskText)
		diagnostics := program.GetSemanticDiagnostics(ctx, program.GetSourceFile("/consumer0/index.ts"))
		assert.Equal(t, len(diagnostics), 1)
		assert.Equal(t, diagnostics[0].Code(), int32(2322))
		definition, err := service.ProvideDefinition(ctx, uri, lsproto.Position{Line: 0, Character: 10})
		assert.NilError(t, err)
		assert.Assert(t, definition.Locations != nil)
		assert.Equal(t, len(*definition.Locations), 1)
		assert.Equal(t, (*definition.Locations)[0].Uri, lsproto.DocumentUri("file:///types/index.d.ts"))
		assert.Equal(t, (*definition.Locations)[0].Range.Start.Line, uint32(2))
		session.WaitForBackgroundTasks()
	}
	checkDisk()
	// The retained request still sees the exact generated declaration/map pair.
	retainedText, ok := oldService.ReadFile(output)
	assert.Assert(t, ok)
	assert.Equal(t, retainedText, oldText)
	_, generated := oldService.GetProgram().GetGeneratedDeclarationText("/types/index.d.ts")
	assert.Assert(t, generated)

	session.DidChangeFile(ctx, uri, 3, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{WholeDocument: &lsproto.TextDocumentContentChangeWholeDocument{Text: consumer + "export const extra = 1;\n"}}})
	checkDisk()
	assert.Equal(t, session.Snapshot().GetDefaultProject(uri).ProgramUpdateKind, ProgramUpdateKindCloned)
}
