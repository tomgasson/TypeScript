package tsctests

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/incremental"
	"github.com/microsoft/TypeScript/tsc/internal/fswatch"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"gotest.tools/v3/assert"
)

const watchDeclarationProducerConfig = `{"compilerOptions":{"composite":true,"isolatedDeclarations":true,"rootDir":".","declarationDir":"../types","module":"esnext","moduleResolution":"bundler"},"files":["index.ts"]}`
const watchDeclarationConsumer = "import { value } from '../producer/index';\nexport const result: number = value() + %d;\n"
const watchDeclarationOutput tspath.RootedFilePath = "/home/src/workspaces/project/types/index.d.ts"

type generatedDeclarationWatchFS struct {
	vfs.FS
	producer      tspath.RootedFilePath
	producerReads atomic.Int64
}

func (f *generatedDeclarationWatchFS) ReadFile(path tspath.RootedFilePath) (string, bool) {
	if path == f.producer {
		f.producerReads.Add(1)
	}
	return f.FS.ReadFile(path)
}

type generatedDeclarationWatchSys struct {
	*TestSys
	reads   *generatedDeclarationWatchFS
	program *incremental.Program
	root    tspath.RootedDirectoryPath
}

func (s *generatedDeclarationWatchSys) FS() vfs.FS { return s.reads }

func (s *generatedDeclarationWatchSys) OnProgram(program *incremental.Program) {
	s.program = program
}

func newGeneratedDeclarationWatcher(t *testing.T) (*execute.Watcher, *generatedDeclarationWatchSys) {
	t.Helper()
	return newGeneratedDeclarationWatcherAt(t, "/home/src/workspaces/project")
}

func newGeneratedDeclarationWatcherAt(t *testing.T, root tspath.RootedDirectoryPath) (*execute.Watcher, *generatedDeclarationWatchSys) {
	t.Helper()
	base := newTestSys(&tscInput{cwd: root.ResolveDirectory("consumer").AsString(), files: FileMap{
		root.ResolveFile("producer/tsconfig.json").AsString(): watchDeclarationProducerConfig,
		root.ResolveFile("producer/index.ts").AsString():      "export function value(): number { return 1; }\n",
		root.ResolveFile("consumer/tsconfig.json").AsString(): `{"compilerOptions":{"noEmit":true,"module":"esnext","moduleResolution":"bundler","disableSourceOfProjectReferenceRedirect":true},"files":["index.ts","../globals.d.ts"],"references":[{"path":"../producer"}]}`,
		root.ResolveFile("consumer/index.ts").AsString():      fmt.Sprintf(watchDeclarationConsumer, 0),
		// A shared root makes producer directory events visible to the mock backend.
		root.ResolveFile("globals.d.ts").AsString(): "",
	}}, false)
	sys := &generatedDeclarationWatchSys{TestSys: base, root: root, reads: &generatedDeclarationWatchFS{FS: base.FS(), producer: root.ResolveFile("producer/index.ts")}}
	result := execute.CommandLine(context.Background(), sys, []string{"--watch", "--pretty", "false"}, sys)
	assert.Assert(t, result.Watcher != nil)
	w := result.Watcher.(*execute.Watcher)
	assert.Assert(t, sys.program != nil)
	assert.Assert(t, sys.program.GetProgram().GetSourceFile(root.ResolveFile("types/index.d.ts")) != nil)
	assert.Assert(t, sys.reads.producerReads.Load() > 0)
	assert.Assert(t, !sys.FS().FileExists(root.ResolveFile("types/index.d.ts")))
	assertGeneratedWatchDiagnostics(t, sys, 0)
	return w, sys
}

func assertGeneratedWatchDiagnostics(t *testing.T, sys *generatedDeclarationWatchSys, code int32) {
	t.Helper()
	program := sys.program.GetProgram()
	file := program.GetSourceFile(sys.root.ResolveFile("consumer/index.ts"))
	diagnostics := program.GetSemanticDiagnostics(context.Background(), file)
	if code == 0 {
		assert.Equal(t, len(diagnostics), 0, "%v", diagnostics)
	} else {
		assert.Equal(t, len(diagnostics), 1, "%v", diagnostics)
		assert.Equal(t, diagnostics[0].Code(), code)
	}
}

func TestWatcherGeneratedDeclarationsConsumerEdit(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcher(t)
	oldProgram := sys.program.GetProgram()
	declaration := oldProgram.GetSourceFile(watchDeclarationOutput)
	reads := sys.reads.producerReads.Load()
	for edit := 1; edit <= 2; edit++ {
		fast, full := w.FastPathBuilds(), w.FullBuilds()
		sys.writeFileNoError("/home/src/workspaces/project/consumer/index.ts", fmt.Sprintf(watchDeclarationConsumer, edit))
		sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/home/src/workspaces/project/consumer/index.ts"}})
		w.DoCycle()
		assert.Equal(t, w.FastPathBuilds(), fast+1, "consumer edit should reuse the generated declaration")
		assert.Equal(t, w.FullBuilds(), full)
		assert.Assert(t, sys.program.GetProgram() != oldProgram)
		assert.Equal(t, sys.program.GetProgram().GetSourceFile(watchDeclarationOutput), declaration)
		assert.Equal(t, sys.reads.producerReads.Load(), reads, "consumer edit must not reread the producer implementation")
		assertGeneratedWatchDiagnostics(t, sys, 0)
		oldProgram = sys.program.GetProgram()
	}
}

func TestWatcherGeneratedDeclarationsProducerChange(t *testing.T) {
	for _, c := range []struct {
		name string
		path tspath.RootedFilePath
		text string
		code int32
	}{
		{"Signature", "/home/src/workspaces/project/producer/index.ts", "export function value(): string { return 'changed'; }\n", 2322},
		{"SignatureDirectory", "/home/src/workspaces/project/producer/index.ts", "export function value(): string { return 'changed'; }\n", 2322},
		{"Config", "/home/src/workspaces/project/producer/tsconfig.json", strings.Replace(watchDeclarationProducerConfig, `"isolatedDeclarations":true`, `"isolatedDeclarations":false`, 1), 6305},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, sys := newGeneratedDeclarationWatcher(t)
			fast, full := w.FastPathBuilds(), w.FullBuilds()
			reads := sys.reads.producerReads.Load()
			sys.writeFileNoError(c.path.AsString(), c.text)
			sys.writeFileNoError("/home/src/workspaces/project/consumer/index.ts", fmt.Sprintf(watchDeclarationConsumer, 1))
			eventPath := c.path.AsString()
			if c.name == "SignatureDirectory" {
				eventPath = c.path.Directory().AsString()
			}
			sys.mockWatchBackend.SendEvents([]fswatch.Event{
				{Kind: fswatch.EventUpdate, Path: eventPath},
				{Kind: fswatch.EventUpdate, Path: "/home/src/workspaces/project/consumer/index.ts"},
			})
			w.DoCycle()
			assert.Equal(t, w.FullBuilds(), full+1)
			assert.Equal(t, w.FastPathBuilds(), fast)
			text, generated := sys.program.GetProgram().GetGeneratedDeclarationText(tspath.PathKey(watchDeclarationOutput))
			assert.Equal(t, generated, c.code == 2322)
			if generated {
				assert.Assert(t, strings.Contains(text, "value(): string"))
				assert.Assert(t, sys.reads.producerReads.Load() > reads)
			}
			assertGeneratedWatchDiagnostics(t, sys, c.code)
		})
	}
}

func TestWatcherGeneratedDeclarationsPhysicalOutput(t *testing.T) {
	for _, sendOutputEvent := range []bool{false, true} {
		t.Run(fmt.Sprintf("OutputEvent=%t", sendOutputEvent), func(t *testing.T) {
			w, sys := newGeneratedDeclarationWatcher(t)
			fast, full := w.FastPathBuilds(), w.FullBuilds()
			const diskText = "export declare function value(): string;\n"
			sys.writeFileNoError(watchDeclarationOutput.AsString(), diskText)
			sys.writeFileNoError("/home/src/workspaces/project/consumer/index.ts", fmt.Sprintf(watchDeclarationConsumer, 1))
			events := []fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/home/src/workspaces/project/consumer/index.ts"}}
			if sendOutputEvent {
				events = append(events, fswatch.Event{Kind: fswatch.EventUpdate, Path: watchDeclarationOutput.AsString()})
			}
			sys.mockWatchBackend.SendEvents(events)
			w.DoCycle()
			assert.Equal(t, w.FullBuilds(), full+1, "a newly physical declaration must reject the consumer fast path")
			assert.Equal(t, w.FastPathBuilds(), fast)
			program := sys.program.GetProgram()
			_, generated := program.GetGeneratedDeclarationText(tspath.PathKey(watchDeclarationOutput))
			assert.Assert(t, !generated)
			assert.Equal(t, program.GetSourceFile(watchDeclarationOutput).Text(), diskText)
			assertGeneratedWatchDiagnostics(t, sys, 2322)
		})
	}
}

func TestWatcherGeneratedDeclarationsShallowInputs(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	assert.Assert(t, !isWatched(sys.TestSys, "/"), "failed lookup locations must not cause a root watch")
	for _, c := range []struct {
		path string
		text string
		code int32
	}{
		{"/workspace/producer/index.ts", "export function value(): string { return 'changed'; }\n", 2322},
		{"/workspace/producer/tsconfig.json", strings.Replace(watchDeclarationProducerConfig, `"isolatedDeclarations":true`, `"isolatedDeclarations":false`, 1), 6305},
		{"/workspace/producer/tsconfig.json", watchDeclarationProducerConfig, 2322},
	} {
		full := w.FullBuilds()
		sys.writeFileNoError(c.path, c.text)
		sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: c.path}})
		w.DoCycle()
		assert.Equal(t, w.FullBuilds(), full+1, "referenced producer input must remain watched: %s", c.path)
		assertGeneratedWatchDiagnostics(t, sys, c.code)
	}
}

func TestWatcherGeneratedDeclarationsExtendedConfig(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	sys.writeFileNoError("/workspace/producer/base.json", watchDeclarationProducerConfig)
	sys.writeFileNoError("/workspace/producer/tsconfig.json", `{"extends":"./base.json"}`)
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/tsconfig.json"}})
	w.DoCycle()
	assertGeneratedWatchDiagnostics(t, sys, 0)
	// A separate rebuild must not drop dependencies satisfied by an old parsed-config cache.
	full := w.FullBuilds()
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer"}})
	w.DoCycle()
	assert.Equal(t, w.FullBuilds(), full+1)
	for _, enabled := range []bool{false, true} {
		full = w.FullBuilds()
		text := strings.Replace(watchDeclarationProducerConfig, `"isolatedDeclarations":true`, fmt.Sprintf(`"isolatedDeclarations":%t`, enabled), 1)
		sys.writeFileNoError("/workspace/producer/base.json", text)
		sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/base.json"}})
		w.DoCycle()
		assert.Equal(t, w.FullBuilds(), full+1, "extended producer config must remain watched across rebuilds")
		_, generated := sys.program.GetProgram().GetGeneratedDeclarationText("/workspace/types/index.d.ts")
		assert.Equal(t, generated, enabled, "extended producer options must be reparsed")
		if enabled {
			assertGeneratedWatchDiagnostics(t, sys, 0)
		} else {
			assertGeneratedWatchDiagnostics(t, sys, 6305)
		}
	}
}

func TestWatcherGeneratedDeclarationsDirectoryRepair(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	sys.writeFileNoError("/workspace/producer/index.ts", "export function value() { return 1 + 1; }\n")
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/index.ts"}})
	w.DoCycle()
	assertGeneratedWatchDiagnostics(t, sys, 6305)
	assert.Assert(t, !sys.program.GetProgram().HasGeneratedDeclarations())
	full := w.FullBuilds()
	sys.writeFileNoError("/workspace/producer/index.ts", "export function value(): number { return 2; }\n")
	sys.writeFileNoError("/workspace/consumer/index.ts", fmt.Sprintf(watchDeclarationConsumer, 1))
	sys.mockWatchBackend.SendEvents([]fswatch.Event{
		{Kind: fswatch.EventUpdate, Path: "/workspace/producer"},
		{Kind: fswatch.EventUpdate, Path: "/workspace/consumer/index.ts"},
	})
	w.DoCycle()
	assert.Equal(t, w.FullBuilds(), full+1)
	assertGeneratedWatchDiagnostics(t, sys, 0)
	assert.Assert(t, sys.program.GetProgram().HasGeneratedDeclarations())
}

func TestWatcherGeneratedDeclarationsMissingConfigRepair(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	sys.removeNoError("/workspace/producer/tsconfig.json")
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventDelete, Path: "/workspace/producer/tsconfig.json"}})
	w.DoCycle()
	assert.Assert(t, !sys.program.GetProgram().HasGeneratedDeclarations())
	full := w.FullBuilds()
	sys.writeFileNoError("/workspace/producer/tsconfig.json", watchDeclarationProducerConfig)
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/tsconfig.json"}})
	w.DoCycle()
	assert.Equal(t, w.FullBuilds(), full+1)
	assertGeneratedWatchDiagnostics(t, sys, 0)
	assert.Assert(t, sys.program.GetProgram().HasGeneratedDeclarations())
}

func TestWatcherGeneratedDeclarationsWildcardInputs(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	config := strings.Replace(watchDeclarationProducerConfig, `"files":["index.ts"]`, `"include":["**/*.ts"]`, 1)
	sys.writeFileNoError("/workspace/producer/tsconfig.json", config)
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/tsconfig.json"}})
	w.DoCycle()
	assertGeneratedWatchDiagnostics(t, sys, 0)
	const added = "/workspace/producer/nested/new.ts"
	full := w.FullBuilds()
	sys.writeFileNoError(added, "export function extra() { return 1 + 1; }\n")
	sys.writeFileNoError("/workspace/consumer/index.ts", fmt.Sprintf(watchDeclarationConsumer, 1))
	// A file-only event must survive recursive watch delivery and dependency
	// filtering, and must prevent the simultaneous consumer edit from cloning.
	sys.mockWatchBackend.SendEvents([]fswatch.Event{
		{Kind: fswatch.EventUpdate, Path: added},
		{Kind: fswatch.EventUpdate, Path: "/workspace/consumer/index.ts"},
	})
	w.DoCycle()
	assert.Equal(t, w.FullBuilds(), full+1)
	assertGeneratedWatchDiagnostics(t, sys, 6305)
	assert.Assert(t, !sys.program.GetProgram().HasGeneratedDeclarations())
	full = w.FullBuilds()
	sys.removeNoError(added)
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventDelete, Path: added}})
	w.DoCycle()
	assert.Equal(t, w.FullBuilds(), full+1)
	assertGeneratedWatchDiagnostics(t, sys, 0)
	assert.Assert(t, sys.program.GetProgram().HasGeneratedDeclarations())
	assert.Assert(t, !isWatched(sys.TestSys, "/"))
}

func TestWatcherGeneratedDeclarationsHiddenShallowInput(t *testing.T) {
	w, sys := newGeneratedDeclarationWatcherAt(t, "/workspace")
	const valid = "export interface Hidden { value: number; }\n"
	sys.writeFileNoError("/shared/types.d.ts", valid)
	sys.writeFileNoError("/workspace/producer/index.ts", "import type { Hidden } from '/shared/types';\nexport function value(): number { const local: Hidden = { value: 1 }; return local.value; }\n")
	sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: fswatch.EventUpdate, Path: "/workspace/producer/index.ts"}})
	w.DoCycle()
	assertGeneratedWatchDiagnostics(t, sys, 0)
	program := sys.program.GetProgram()
	text, generated := program.GetGeneratedDeclarationText("/workspace/types/index.d.ts")
	assert.Assert(t, generated)
	assert.Assert(t, !strings.Contains(text, "/shared/types"))
	assert.Assert(t, program.GetSourceFile("/shared/types.d.ts") == nil, "implementation-only dependency must stay outside the consumer program")
	for _, c := range []struct {
		text string
		code int32
	}{
		{"export interface Hidden { value: ; }\n", 6305},
		{valid, 0},
		{"", 0}, // A missing body-only import is a semantic error, so declaration emit still succeeds.
		{"export interface Hidden { value: ; }\n", 6305},
		{valid, 0},
	} {
		full := w.FullBuilds()
		kind := fswatch.EventUpdate
		if c.text == "" {
			sys.removeNoError("/shared/types.d.ts")
			kind = fswatch.EventDelete
		} else {
			sys.writeFileNoError("/shared/types.d.ts", c.text)
		}
		sys.mockWatchBackend.SendEvents([]fswatch.Event{{Kind: kind, Path: "/shared/types.d.ts"}})
		w.DoCycle()
		assert.Equal(t, w.FullBuilds(), full+1, "actual shallow input must remain watched even outside producer roots and emitted declarations")
		assertGeneratedWatchDiagnostics(t, sys, c.code)
	}
	assert.Assert(t, !isWatched(sys.TestSys, "/"))
}
