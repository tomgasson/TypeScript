package project

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
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

type editorBoundaryFixture struct {
	fs          vfs.FS
	producer    *tsoptions.ParsedCommandLine
	text        string
	sourceBytes int
}

// Match the compiler boundary workload, with a one-character consumer edit.
// Generated declarations use the real producer config and normal project
// reference output identities. This is not automatic signature fallback.
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
	fs := vfstest.FromMap(files, tspath.CaseSensitive)
	parse := func(name tspath.RootedFilePath) *tsoptions.ParsedCommandLine {
		config, diagnostics := tsoptions.GetParsedCommandLineOfConfigFile(name, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
		if config == nil || len(diagnostics) != 0 || len(config.Errors) != 0 {
			tb.Fatalf("invalid benchmark config %s: %v", name, diagnostics)
		}
		return config
	}
	return editorBoundaryFixture{fs: fs, producer: parse("/producer/tsconfig.json"), text: consumer.String(), sourceBytes: sourceBytes}
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
		output := tspath.RootedFilePath("/producer/types/" + stem + ".d.ts")
		name, absent := source, output
		if declarations {
			name, absent = output, source
		}
		file := p.GetSourceFile(name)
		if file == nil || file.IsDeclarationFile != declarations || p.GetSourceFile(absent) != nil {
			tb.Fatalf("wrong project-reference route for %s", name)
		}
		reference := p.GetProjectReferenceFromSource(file.PathKey())
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
	for _, declarations := range []bool{false, true} {
		f := newEditorBoundaryFixture(t, declarations)
		if declarations {
			f.generate(t)
		}
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
		t.Logf("declarations=%t: 16 edits reused all %d dependency trees; live parse-cache entries=%d", declarations, len(dependencies), s.parseCache.entries.Size())
		s.Close()
		if s.parseCache.entries.Size() != 0 || s.programCounter.Len() != 0 {
			t.Fatal("session close did not release parse-cache/program ownership")
		}
	}
}

// In-process LSP session path: a partial consumer edit, snapshot update, pull
// diagnostics and background-queue drain. Unchanged dependency ASTs are reused
// in both arms (verified above). No JSON/socket transport, producer edits,
// automatic declaration fallback or generation cost is measured. The compiler
// boundary benchmark measures declaration production separately.
func BenchmarkEditorDeclarationBoundary(b *testing.B) {
	for _, declarations := range []bool{false, true} {
		b.Run(core.IfElse(declarations, "Declarations", "SourceDependencies"), func(b *testing.B) {
			f := newEditorBoundaryFixture(b, declarations)
			if declarations {
				f.generate(b)
			}
			s := f.open()
			defer s.Close()
			editorBoundaryDiagnostics(b, s)
			editorBoundaryDependencies(b, s, declarations)
			version := int32(2)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				editDeclarationBoundary(b, s, version)
				version++
			}
		})
	}
}

// Run separately with -benchtime=1x. Retain four independent Sessions after
// 16 consumer edits each, including real snapshot/parse-cache/checker ownership.
// The common source VFS/config exists before the baseline in both arms; emitted
// output is produced afterwards and included once, shared across all sessions.
// Old snapshots are released normally; this excludes concurrent outstanding
// requests. The metric is post-GC marginal heap before idle checker eviction,
// not RSS/peak heap. Declaration generation is outside the edit timing above.
func BenchmarkEditorDeclarationBoundaryRetainedHeap(b *testing.B) {
	for _, declarations := range []bool{false, true} {
		b.Run(core.IfElse(declarations, "Declarations", "SourceDependencies"), func(b *testing.B) {
			if b.N != 1 {
				b.Skip("retained-heap sampling requires -benchtime=1x")
			}
			warm := newEditorBoundaryFixture(b, declarations)
			if declarations {
				warm.generate(b)
			}
			warmSession := warm.open()
			editDeclarationBoundary(b, warmSession, 2)
			warmSession.Close()
			warm, warmSession = editorBoundaryFixture{}, nil
			f := newEditorBoundaryFixture(b, declarations)
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
			outputBytes := 0
			if declarations {
				outputBytes = f.generate(b)
			}
			for i := range sessions {
				sessions[i] = f.open()
				for version := int32(2); version <= 17; version++ {
					editDeclarationBoundary(b, sessions[i], version)
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
			runtime.KeepAlive(sessions)
			live := float64(int64(after.HeapAlloc) - int64(before.HeapAlloc))
			b.ReportMetric(live, "live-B/batch")
			b.ReportMetric(live/batchSize, "live-B/session")
			b.ReportMetric(float64(outputBytes), "shared-declaration-B")
			b.ReportMetric(editorBoundaryModules+3, "cache-entries/session")
		})
	}
}
