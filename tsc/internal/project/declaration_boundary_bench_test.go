package project

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

const editorBoundaryModules = 48
const editorBoundaryURI lsproto.DocumentUri = "file:///consumer/index.ts"

type editorBoundaryMode string

const (
	editorBoundarySource    editorBoundaryMode = "SourceDependencies"
	editorBoundaryPrepared  editorBoundaryMode = "PreparedDeclarations"
	editorBoundaryAutomatic editorBoundaryMode = "AutomaticDeclarations"
)

var editorBoundaryModes = []editorBoundaryMode{editorBoundarySource, editorBoundaryPrepared, editorBoundaryAutomatic}

type editorBoundaryReads struct {
	sourceFiles atomic.Int64
	sourceBytes atomic.Int64
}

type editorBoundaryCountingFS struct {
	vfs.FS
	reads *editorBoundaryReads
}

func (fs *editorBoundaryCountingFS) ReadFile(name tspath.RootedFilePath) (string, bool) {
	text, ok := fs.FS.ReadFile(name)
	if ok && strings.HasPrefix(name.AsString(), "/producer/src/") {
		fs.reads.sourceFiles.Add(1)
		fs.reads.sourceBytes.Add(int64(len(text)))
	}
	return text, ok
}

type editorBoundaryFixture struct {
	fs          vfs.FS
	producer    *tsoptions.ParsedCommandLine
	text        string
	sourceBytes int
	reads       *editorBoundaryReads
}

// Match the compiler boundary workload, with a one-character consumer edit.
// Generated declarations use the real producer config and normal project
// reference output identities. The automatic arm leaves output absent and lets
// ordinary project-reference loading produce it. The read counters observe only
// underlying source reads, not extraction calls or artifact-cache lookups.
func newEditorBoundaryFixture(tb testing.TB, declarations bool) editorBoundaryFixture {
	tb.Helper()
	files := map[string]string{
		"/globals.d.ts":           `interface Object {} interface Function {} interface CallableFunction {} interface NewableFunction {} interface IArguments {} interface String {} interface Number {} interface Boolean {} interface RegExp {} interface Array<T> { length: number; [n: number]: T; }`,
		"/consumer/tsconfig.json": `{"compilerOptions":{"noLib":true,"strict":true,"noEmit":true,"module":"esnext","moduleResolution":"bundler","target":"es2020"},"files":["index.ts","../globals.d.ts"],"references":[{"path":"../producer"}]}`,
	}
	var barrel, consumer, roots strings.Builder
	consumer.WriteString("export const edit: number = 0;\n")
	roots.WriteString(`"src/index.ts","../globals.d.ts"`)
	var sourceBytes int
	for i := range editorBoundaryModules {
		var source strings.Builder
		fmt.Fprintf(&source, "export interface Value%d { value: number; }\nexport function read%d(input: Value%d): number {\n", i, i, i)
		for j := range 256 {
			fmt.Fprintf(&source, "  const local%d: number = input.value + %d;\n", j, j)
		}
		source.WriteString("  return local255;\n}\n")
		files[fmt.Sprintf("/producer/src/module%d.ts", i)] = source.String()
		sourceBytes += source.Len()
		fmt.Fprintf(&roots, `,"src/module%d.ts"`, i)
		fmt.Fprintf(&barrel, "export * from './module%d';\n", i)
		fmt.Fprintf(&consumer, "import { read%d, type Value%d } from '../producer/src/index';\nconst input%d: Value%d = { value: edit + %d };\nexport const result%d: number = read%d(input%d);\n", i, i, i, i, i, i, i, i)
	}
	// A nonempty diagnostic result checks more than merely successful loading.
	consumer.WriteString("export const mismatch: string = read0(input0);\n")
	files["/producer/src/index.ts"] = barrel.String()
	sourceBytes += barrel.Len()
	files["/consumer/index.ts"] = consumer.String()
	files["/producer/tsconfig.json"] = `{"compilerOptions":{"noLib":true,"strict":true,"composite":true,"declaration":true,"isolatedDeclarations":true,"module":"esnext","moduleResolution":"bundler","target":"es2020","rootDir":"src","declarationDir":"types"},"files":[` + roots.String() + `]}`
	if declarations {
		files["/consumer/tsconfig.json"] = strings.Replace(files["/consumer/tsconfig.json"], `"noLib":true`, `"noLib":true,"disableSourceOfProjectReferenceRedirect":true`, 1)
	}
	reads := &editorBoundaryReads{}
	fs := &editorBoundaryCountingFS{FS: vfstest.FromMap(files, tspath.CaseSensitive), reads: reads}
	parse := func(name tspath.RootedFilePath) *tsoptions.ParsedCommandLine {
		config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(name, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
		if config == nil || len(diagnostics) != 0 || len(config.Errors) != 0 {
			tb.Fatalf("invalid benchmark config %s: %v", name, diagnostics)
		}
		return config
	}
	return editorBoundaryFixture{fs: fs, producer: parse("/producer/tsconfig.json"), text: consumer.String(), sourceBytes: sourceBytes, reads: reads}
}

func (f editorBoundaryFixture) prepare(tb testing.TB, mode editorBoundaryMode) int {
	if mode == editorBoundaryPrepared {
		return f.generate(tb)
	}
	return 0
}

func (f editorBoundaryFixture) generate(tb testing.TB) int {
	tb.Helper()
	p := compiler.NewProgram(compiler.ProgramOptions{Config: f.producer, Host: compiler.NewCompilerHost(f.fs, "/", nil, nil, nil), SingleThreaded: core.TSTrue})
	count, bytes := 0, 0
	result := p.Emit(context.Background(), compiler.EmitOptions{
		EmitOnly: compiler.EmitOnlyDts,
		WriteFile: func(name tspath.RootedFilePath, text string, _ *compiler.WriteFileData) error {
			if !strings.HasPrefix(name.AsString(), "/producer/types/") || !strings.HasSuffix(name.AsString(), ".d.ts") {
				tb.Fatalf("unexpected output %s", name)
			}
			count++
			bytes += len(text)
			return f.fs.WriteFile(name, text)
		},
	})
	if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 || count != editorBoundaryModules+1 {
		tb.Fatalf("declaration generation failed: %d outputs, result %+v", count, result)
	}
	return bytes
}

func (f editorBoundaryFixture) open() *Session {
	s := NewSession(&SessionInit{
		BackgroundCtx: context.Background(), FS: f.fs, Client: noopClient{},
		Options: &SessionOptions{CurrentDirectory: "/", DefaultLibraryPath: "/", PositionEncoding: lsproto.PositionEncodingKindUTF8},
	})
	preferences := s.Config()
	preferences.IncludeCompletionsForModuleExports = core.TSFalse
	s.InitializeWithUserConfig(preferences)
	s.DidOpenFile(context.Background(), editorBoundaryURI, 1, f.text, lsproto.LanguageKindTypeScript)
	s.WaitForBackgroundTasks()
	return s
}

func editorBoundaryDiagnostics(tb testing.TB, s *Session) []*lsproto.Diagnostic {
	tb.Helper()
	ctx := core.WithCheckerLifetime(context.Background(), core.CheckerLifetimeDiagnostics)
	service, err := s.GetLanguageService(ctx, editorBoundaryURI)
	if err != nil {
		tb.Fatal(err)
	}
	result, err := service.ProvideDiagnostics(ctx, editorBoundaryURI)
	if err != nil || result.FullDocumentDiagnosticReport == nil {
		tb.Fatalf("pull diagnostics failed: %v", err)
	}
	return result.FullDocumentDiagnosticReport.Items
}

func editDeclarationBoundary(tb testing.TB, s *Session, version int32) []*lsproto.Diagnostic {
	// Alternate the exported numeric literal while keeping all dependency text,
	// import specifiers and public consumer types unchanged.
	s.DidChangeFile(context.Background(), editorBoundaryURI, version, []lsproto.TextDocumentContentChangePartialOrWholeDocument{{
		Partial: &lsproto.TextDocumentContentChangePartial{
			Range: lsproto.Range{Start: lsproto.Position{Line: 0, Character: 28}, End: lsproto.Position{Line: 0, Character: 29}},
			Text:  core.IfElse(version%2 == 0, "1", "0"),
		},
	}})
	result := editorBoundaryDiagnostics(tb, s)
	// Bound the queue so historical snapshots cannot accumulate behind the
	// measured edits. Logging, watchers, push diagnostics, ATA and auto-import
	// warming are disabled; this includes the remaining queue-drain overhead.
	s.WaitForBackgroundTasks()
	return result
}

func editorBoundaryDependencies(tb testing.TB, s *Session, declarations bool) []*ast.SourceFile {
	tb.Helper()
	p := s.Snapshot().GetDefaultProject(editorBoundaryURI).Program
	files := make([]*ast.SourceFile, 0, editorBoundaryModules+1)
	for i := -1; i < editorBoundaryModules; i++ {
		stem := "index"
		if i >= 0 {
			stem = fmt.Sprintf("module%d", i)
		}
		source := tspath.RootedFilePath("/producer/src/" + stem + ".ts")
		reference := p.GetProjectReferenceFromSource(tspath.PathKey(source))
		if reference == nil {
			tb.Fatalf("missing producer identity for %s", source)
		}
		output := reference.OutputDts
		name, absent := source, output
		if declarations {
			name, absent = output, source
		}
		file := p.GetSourceFile(name)
		if file == nil || file.IsDeclarationFile != declarations || p.GetSourceFile(absent) != nil {
			tb.Fatalf("wrong project-reference route for %s", name)
		}
		if declarations {
			reference = p.GetProjectReferenceFromOutputDts(file.PathKey())
		}
		if reference == nil || reference.Source != source || reference.Resolved.ConfigFileName() != "/producer/tsconfig.json" || !reference.Resolved.CompilerOptions().IsolatedDeclarations.IsTrue() {
			tb.Fatalf("producer identity/options lost for %s", name)
		}
		if !s.parseCache.Has(parseCacheKeyForFile(file)) {
			tb.Fatalf("dependency missing from snapshot parse cache: %s", name)
		}
		files = append(files, file)
	}
	if declarations {
		for key := range s.parseCache.entries.Keys() {
			if strings.HasPrefix(key.FileName.AsString(), "/producer/src/") {
				tb.Fatalf("producer implementation retained in another project's cache: %s", key.FileName)
			}
		}
	}
	return files
}

func TestEditorDeclarationBoundaryReuse(t *testing.T) {
	var expected []*lsproto.Diagnostic
	for _, mode := range editorBoundaryModes {
		declarations := mode != editorBoundarySource
		f := newEditorBoundaryFixture(t, declarations)
		f.prepare(t, mode)
		s := f.open()
		defer func() {
			if s.Snapshot() != nil {
				s.Close()
			}
		}()
		diagnostics := editorBoundaryDiagnostics(t, s)
		if len(diagnostics) != 1 || diagnostics[0].Code == nil || diagnostics[0].Code.Integer == nil || *diagnostics[0].Code.Integer != 2322 {
			t.Fatalf("expected one TS2322, got %+v", diagnostics)
		}
		if expected == nil {
			expected = diagnostics
		} else if !reflect.DeepEqual(expected, diagnostics) {
			t.Fatal("source and declaration LSP diagnostics differ")
		}
		dependencies := editorBoundaryDependencies(t, s, declarations)
		producerReads := f.reads.sourceFiles.Load()
		for version := int32(2); version <= 17; version++ {
			oldSnapshot := s.Snapshot()
			oldProject := oldSnapshot.GetDefaultProject(editorBoundaryURI)
			oldFile := oldProject.Program.GetSourceFile("/consumer/index.ts")
			oldKey := parseCacheKeyForFile(oldFile)
			diagnostics = editDeclarationBoundary(t, s, version)
			if !reflect.DeepEqual(expected, diagnostics) {
				t.Fatalf("diagnostics changed after consumer edit %d", version)
			}
			current := s.Snapshot()
			currentProject := current.GetDefaultProject(editorBoundaryURI)
			if current == oldSnapshot || currentProject.ID() != oldProject.ID() || currentProject.Program.GetSourceFile("/consumer/index.ts") == oldFile {
				t.Fatal("edit did not replace the consumer snapshot/tree while preserving project identity")
			}
			if currentProject.ProgramUpdateKind != ProgramUpdateKindCloned {
				t.Fatalf("consumer edit rebuilt instead of cloning its program: %v", currentProject.ProgramUpdateKind)
			}
			if s.parseCache.Has(oldKey) {
				t.Fatal("superseded consumer parse-cache entry retained")
			}
			if !s.parseCache.Has(parseCacheKeyForFile(currentProject.Program.GetSourceFile("/consumer/index.ts"))) {
				t.Fatal("current consumer parse-cache entry missing")
			}
			for i, file := range editorBoundaryDependencies(t, s, declarations) {
				if file != dependencies[i] {
					t.Fatalf("unchanged dependency %s was reparsed", file.FileName())
				}
			}
			if s.parseCache.entries.Size() != editorBoundaryModules+3 {
				t.Fatalf("expected 49 dependencies, consumer and globals; cache has %d entries", s.parseCache.entries.Size())
			}
		}
		if f.reads.sourceFiles.Load() != producerReads {
			t.Fatal("consumer-only edits reread producer implementations")
		}
		if mode == editorBoundaryAutomatic && f.fs.FileExists("/producer/types/index.d.ts") {
			t.Fatal("automatic declarations were written to the underlying filesystem")
		}
		t.Logf("mode=%s: 16 edits reused all %d dependency trees; producer source rereads=0; live parse-cache entries=%d", mode, len(dependencies), s.parseCache.entries.Size())
		s.Close()
		if s.parseCache.entries.Size() != 0 || s.programCounter.Len() != 0 {
			t.Fatal("session close did not release parse-cache/program ownership")
		}
	}
}

type editorBoundaryProducerChange struct {
	fileName tspath.RootedFilePath
	text     [2]string
}

// Keep all changes in the existing 49-file producer. The transitive case changes
// a leaf type while the forwarding declaration and barrel text stay unchanged.
func newEditorBoundaryProducerChange(tb testing.TB, declarations bool, kind string) (editorBoundaryFixture, editorBoundaryProducerChange) {
	tb.Helper()
	f := newEditorBoundaryFixture(tb, declarations)
	read := func(name tspath.RootedFilePath) string {
		text, ok := f.fs.ReadFile(name)
		if !ok {
			tb.Fatalf("missing fixture file %s", name)
		}
		return text
	}
	write := func(name tspath.RootedFilePath, text string) {
		if err := f.fs.WriteFile(name, text); err != nil {
			tb.Fatal(err)
		}
	}
	change := editorBoundaryProducerChange{fileName: "/producer/src/module0.ts"}
	switch kind {
	case "BodyOnly":
		change.text[0] = read(change.fileName)
		change.text[1] = strings.Replace(change.text[0], "input.value + 0;", "input.value + 1;", 1)
	case "PublicSignature":
		change.text[0] = read(change.fileName)
		change.text[1] = strings.Replace(change.text[0], "): number {", "): number | string {", 1)
	case "ProducerConfig":
		change.fileName = "/producer/tsconfig.json"
		change.text[0] = read(change.fileName)
		change.text[1] = strings.Replace(change.text[0], `"declarationDir":"types"`, `"declarationDir":"types-next"`, 1)
	case "TransitiveSignature":
		forward := `export type Forwarded = import("./module1").Token;` + "\n"
		write("/producer/src/module0.ts", forward+read("/producer/src/module0.ts"))
		change.fileName = "/producer/src/module1.ts"
		leaf := "export interface Token { value: number; }\n"
		change.text[0] = leaf + read(change.fileName)
		change.text[1] = strings.Replace(change.text[0], leaf, "export interface Token { value: number | string; }\n", 1)
		write(change.fileName, change.text[0])
		f.sourceBytes += len(forward) + len(leaf)
		f.text += "\nimport type { Forwarded } from '../producer/src/module0';\nconst forwarded: Forwarded = { value: 1 };\nexport const forwardedValue: number = forwarded.value;\n"
		write("/consumer/index.ts", f.text)
	default:
		tb.Fatalf("unknown producer change %s", kind)
	}
	if change.text[0] == change.text[1] {
		tb.Fatal("producer change did not modify the fixture")
	}
	return f, change
}

func changeEditorBoundaryProducer(tb testing.TB, f editorBoundaryFixture, s *Session, change editorBoundaryProducerChange, state int) []*lsproto.Diagnostic {
	tb.Helper()
	if err := f.fs.WriteFile(change.fileName, change.text[state]); err != nil {
		tb.Fatal(err)
	}
	s.DidChangeWatchedFiles(context.Background(), []*lsproto.FileEvent{{
		Type: lsproto.FileChangeTypeChanged,
		Uri:  lsproto.DocumentUri("file://" + change.fileName.AsString()),
	}})
	diagnostics := editorBoundaryDiagnostics(tb, s)
	s.WaitForBackgroundTasks()
	return diagnostics
}

func TestEditorDeclarationBoundaryProducerChanges(t *testing.T) {
	for _, kind := range []string{"BodyOnly", "PublicSignature", "ProducerConfig", "TransitiveSignature"} {
		t.Run(kind, func(t *testing.T) {
			var expected [2][]*lsproto.Diagnostic
			for _, mode := range []editorBoundaryMode{editorBoundarySource, editorBoundaryAutomatic} {
				t.Run(string(mode), func(t *testing.T) {
					declarations := mode == editorBoundaryAutomatic
					f, change := newEditorBoundaryProducerChange(t, declarations, kind)
					s := f.open()
					defer s.Close()
					editorBoundaryDiagnostics(t, s)
					for update := range 8 {
						state := (update + 1) % 2
						before := editorBoundaryDependencies(t, s, declarations)
						oldProgram := s.Snapshot().GetDefaultProject(editorBoundaryURI).Program
						diagnostics := changeEditorBoundaryProducer(t, f, s, change, state)
						wantDiagnostics := 1
						if state == 1 && (kind == "PublicSignature" || kind == "TransitiveSignature") {
							wantDiagnostics++
						}
						if len(diagnostics) != wantDiagnostics {
							t.Fatalf("%s state %d: got %d diagnostics, want %d", mode, state, len(diagnostics), wantDiagnostics)
						}
						if expected[state] == nil {
							expected[state] = diagnostics
						} else if !reflect.DeepEqual(expected[state], diagnostics) {
							t.Fatalf("%s state %d: source/automatic diagnostics differ or became stale", mode, state)
						}
						project := s.Snapshot().GetDefaultProject(editorBoundaryURI)
						if project.Program == oldProgram {
							t.Fatal("producer change did not update the consumer program")
						}
						after := editorBoundaryDependencies(t, s, declarations)
						reused := 0
						for i, file := range after {
							if file == before[i] {
								reused++
							} else if s.parseCache.Has(parseCacheKeyForFile(before[i])) {
								t.Fatal("superseded dependency remains in the parse cache")
							}
						}
						wantReused := editorBoundaryModules
						if kind == "ProducerConfig" {
							wantReused = core.IfElse(declarations, 0, editorBoundaryModules+1)
							wantPrefix := core.IfElse(state == 1, "/producer/types-next/", "/producer/types/")
							reference := project.Program.GetProjectReferenceFromSource("/producer/src/module0.ts")
							if !strings.HasPrefix(reference.OutputDts.AsString(), wantPrefix) {
								t.Fatal("producer output identity did not follow its changed configuration")
							}
						} else if declarations && kind == "BodyOnly" {
							wantReused++
						}
						if reused != wantReused {
							t.Fatalf("%s state %d: reused %d dependency trees, want %d", mode, state, reused, wantReused)
						}
						if s.parseCache.entries.Size() != editorBoundaryModules+3 {
							t.Fatal("producer changes accumulated historical implementation/declaration cache entries")
						}
						if declarations && (f.fs.FileExists("/producer/types/index.d.ts") || f.fs.FileExists("/producer/types-next/index.d.ts")) {
							t.Fatal("automatic declarations were written to the underlying filesystem")
						}
					}
				})
			}
		})
	}
}

func TestEditorDeclarationBoundaryPhysicalOutputLifecycle(t *testing.T) {
	f := newEditorBoundaryFixture(t, true)
	s := f.open()
	defer s.Close()
	originalDiagnostics := editorBoundaryDiagnostics(t, s)
	const output tspath.RootedFilePath = "/producer/types/module0.d.ts"
	program := s.Snapshot().GetDefaultProject(editorBoundaryURI).Program
	generated, ok := program.GetGeneratedDeclarationText(tspath.PathKey(output))
	if !ok || len(originalDiagnostics) != 1 {
		t.Fatal("expected generated output and the original consumer diagnostic")
	}
	physical := strings.Replace(generated, "): number;", "): string;", 1)
	if physical == generated {
		t.Fatal("physical output must differ from the generated public signature")
	}
	if err := f.fs.WriteFile(output, physical); err != nil {
		t.Fatal(err)
	}
	s.DidChangeWatchedFiles(context.Background(), []*lsproto.FileEvent{{
		Type: lsproto.FileChangeTypeCreated,
		Uri:  lsproto.DocumentUri("file://" + output.AsString()),
	}})
	diagnostics := editorBoundaryDiagnostics(t, s)
	s.WaitForBackgroundTasks()
	snapshot := s.Snapshot()
	program = snapshot.GetDefaultProject(editorBoundaryURI).Program
	if _, ok := program.GetGeneratedDeclarationText(tspath.PathKey(output)); ok {
		t.Fatal("new physical declaration still has a stale generated representation")
	}
	if text, ok := snapshot.LanguageServiceHost(program).ReadFile(output); !ok || text != physical {
		t.Fatal("program-scoped language service read did not select physical output")
	}
	if file := program.GetSourceFile(output); file == nil || file.Text() != physical {
		t.Fatal("consumer program did not load the physical declaration")
	}
	// Returning string fixes the final mismatch and instead makes result0 fail.
	if len(diagnostics) != 1 || diagnostics[0].Range.Start.Line != 3 || reflect.DeepEqual(originalDiagnostics, diagnostics) {
		t.Fatal("consumer diagnostics did not follow the new physical signature")
	}
	if err := f.fs.Remove(output.AsPath()); err != nil {
		t.Fatal(err)
	}
	s.DidChangeWatchedFiles(context.Background(), []*lsproto.FileEvent{{
		Type: lsproto.FileChangeTypeDeleted,
		Uri:  lsproto.DocumentUri("file://" + output.AsString()),
	}})
	diagnostics = editorBoundaryDiagnostics(t, s)
	s.WaitForBackgroundTasks()
	snapshot = s.Snapshot()
	program = snapshot.GetDefaultProject(editorBoundaryURI).Program
	if text, ok := program.GetGeneratedDeclarationText(tspath.PathKey(output)); !ok || text != generated {
		t.Fatal("deleting physical output did not restore current generated output")
	}
	if text, ok := snapshot.LanguageServiceHost(program).ReadFile(output); !ok || text != generated {
		t.Fatal("program-scoped read retained physical text after deletion")
	}
	if !reflect.DeepEqual(originalDiagnostics, diagnostics) {
		t.Fatal("consumer diagnostics did not return to the generated-signature result")
	}
}

// Each operation alternates a real producer edit, processes its watched-file
// notification, checks the consumer and drains background work. Automatic output
// regeneration, when needed, is inside the timed operation. Prepared output is
// omitted: leaving it unchanged after a producer edit would measure stale data.
func BenchmarkEditorDeclarationProducerChanges(b *testing.B) {
	for _, kind := range []string{"BodyOnly", "PublicSignature", "ProducerConfig", "TransitiveSignature"} {
		b.Run(kind, func(b *testing.B) {
			for _, mode := range []editorBoundaryMode{editorBoundarySource, editorBoundaryAutomatic} {
				b.Run(string(mode), func(b *testing.B) {
					f, change := newEditorBoundaryProducerChange(b, mode == editorBoundaryAutomatic, kind)
					s := f.open()
					defer s.Close()
					editorBoundaryDiagnostics(b, s)
					editorBoundaryDependencies(b, s, mode == editorBoundaryAutomatic)
					wantChangedDiagnostics := 1
					if kind == "PublicSignature" || kind == "TransitiveSignature" {
						wantChangedDiagnostics++
					}
					if diagnostics := changeEditorBoundaryProducer(b, f, s, change, 1); len(diagnostics) != wantChangedDiagnostics {
						b.Fatal("producer edit preflight did not update consumer diagnostics")
					}
					if diagnostics := changeEditorBoundaryProducer(b, f, s, change, 0); len(diagnostics) != 1 {
						b.Fatal("producer edit preflight did not restore consumer diagnostics")
					}
					readsBefore := f.reads.sourceFiles.Load()
					bytesBefore := f.reads.sourceBytes.Load()
					state := 1
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						changeEditorBoundaryProducer(b, f, s, change, state)
						state = 1 - state
					}
					b.ReportMetric(float64(f.reads.sourceFiles.Load()-readsBefore)/float64(b.N), "producer-source-reads/op")
					b.ReportMetric(float64(f.reads.sourceBytes.Load()-bytesBefore)/float64(b.N), "producer-source-read-B/op")
				})
			}
		})
	}
}

// In-process LSP session path: a partial consumer edit, snapshot update, pull
// diagnostics and background-queue drain. Unchanged dependency ASTs are reused
// in all arms (verified above). No JSON/socket transport or producer edits are
// measured here. Automatic generation occurs while opening
// the session, outside these steady-state consumer edits. Program cloning and
// unchanged dependency pointers demonstrate reuse; underlying FS reads alone do
// not count extraction calls or declaration-cache hits.
func BenchmarkEditorDeclarationBoundary(b *testing.B) {
	for _, mode := range editorBoundaryModes {
		b.Run(string(mode), func(b *testing.B) {
			declarations := mode != editorBoundarySource
			f := newEditorBoundaryFixture(b, declarations)
			f.prepare(b, mode)
			s := f.open()
			defer s.Close()
			editorBoundaryDiagnostics(b, s)
			editorBoundaryDependencies(b, s, declarations)
			readsBefore := f.reads.sourceFiles.Load()
			bytesBefore := f.reads.sourceBytes.Load()
			version := int32(2)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				editDeclarationBoundary(b, s, version)
				version++
			}
			b.ReportMetric(float64(f.reads.sourceFiles.Load()-readsBefore)/float64(b.N), "producer-source-reads/op")
			b.ReportMetric(float64(f.reads.sourceBytes.Load()-bytesBefore)/float64(b.N), "producer-source-read-B/op")
		})
	}
}

// Run separately with -benchtime=1x. Retain four independent Sessions after
// 16 consumer edits each, including real snapshot/parse-cache/checker ownership.
// The common source VFS/config exists before the baseline in both arms; emitted
// prepared output is produced afterwards and included once, shared across all
// sessions. Automatic output is created separately by each session after the
// baseline; that session's retained output and tracking state are included.
// Old snapshots are released normally; this excludes concurrent outstanding
// requests. The metric is post-GC marginal heap before idle checker eviction,
// not RSS/peak heap. Declaration generation is outside the edit timing above.
func BenchmarkEditorDeclarationBoundaryRetainedHeap(b *testing.B) {
	benchmarkEditorDeclarationRetainedHeap(b, "")
}

func BenchmarkEditorDeclarationProducerRetainedHeap(b *testing.B) {
	for _, kind := range []string{"BodyOnly", "PublicSignature"} {
		b.Run(kind, func(b *testing.B) {
			benchmarkEditorDeclarationRetainedHeap(b, kind)
		})
	}
}

func benchmarkEditorDeclarationRetainedHeap(b *testing.B, producerChange string) {
	modes := editorBoundaryModes
	if producerChange != "" {
		modes = []editorBoundaryMode{editorBoundarySource, editorBoundaryAutomatic}
	}
	for _, mode := range modes {
		b.Run(string(mode), func(b *testing.B) {
			declarations := mode != editorBoundarySource
			if b.N != 1 {
				b.Skip("retained-heap sampling requires -benchtime=1x")
			}
			newFixture := func() (editorBoundaryFixture, editorBoundaryProducerChange) {
				if producerChange != "" {
					return newEditorBoundaryProducerChange(b, declarations, producerChange)
				}
				return newEditorBoundaryFixture(b, declarations), editorBoundaryProducerChange{}
			}
			applyEdit := func(f editorBoundaryFixture, s *Session, change editorBoundaryProducerChange, version int32) {
				if producerChange != "" {
					changeEditorBoundaryProducer(b, f, s, change, 1-int(version%2))
				} else {
					editDeclarationBoundary(b, s, version)
				}
			}
			warm, warmChange := newFixture()
			warm.prepare(b, mode)
			warmSession := warm.open()
			applyEdit(warm, warmSession, warmChange, 2)
			warmSession.Close()
			warm, warmSession = editorBoundaryFixture{}, nil
			warmChange = editorBoundaryProducerChange{}
			f, change := newFixture()
			const batchSize = 4
			sessions := make([]*Session, batchSize)
			defer func() {
				for _, s := range sessions {
					if s != nil {
						s.Close()
					}
				}
			}()
			runtime.GC()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ResetTimer()
			outputBytes := f.prepare(b, mode)
			for i := range sessions {
				sessions[i] = f.open()
				for version := int32(2); version <= 17; version++ {
					applyEdit(f, sessions[i], change, version)
				}
				if sessions[i].parseCache.entries.Size() != editorBoundaryModules+3 {
					b.Fatal("unexpected historical parse-cache entries after edits")
				}
			}
			b.StopTimer()
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(f)
			runtime.KeepAlive(change)
			runtime.KeepAlive(sessions)
			live := float64(int64(after.HeapAlloc) - int64(before.HeapAlloc))
			b.ReportMetric(live, "live-B/batch")
			b.ReportMetric(live/batchSize, "live-B/session")
			b.ReportMetric(float64(outputBytes), "shared-declaration-B")
			b.ReportMetric(editorBoundaryModules+3, "cache-entries/session")
			var declarationTextBytes int
			for _, s := range sessions {
				for _, file := range s.Snapshot().GetDefaultProject(editorBoundaryURI).Program.SourceFiles() {
					if file.IsDeclarationFile && strings.HasPrefix(file.FileName().AsString(), "/producer/") {
						declarationTextBytes += len(file.Text())
					}
				}
			}
			// Logical text size: prepared text is shared, automatic text is owned
			// independently per session. Heap deltas above include actual storage.
			b.ReportMetric(float64(declarationTextBytes)/batchSize, "declaration-text-B/session")
		})
	}
}
