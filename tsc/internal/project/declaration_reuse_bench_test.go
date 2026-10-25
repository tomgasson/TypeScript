package project

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func editorDeclarationGenerationFixture(tb testing.TB, noEmitOnError bool, changeKind string) (editorBoundaryFixture, editorBoundaryProducerChange) {
	tb.Helper()
	var f editorBoundaryFixture
	var change editorBoundaryProducerChange
	if changeKind == "" {
		f = newEditorBoundaryFixture(tb, true)
	} else {
		f, change = newEditorBoundaryProducerChange(tb, true, changeKind)
	}
	if noEmitOnError {
		const name = "/producer/tsconfig.json"
		withNoEmitOnError := func(text string) string {
			changed := strings.Replace(text, `"declaration":true`, `"declaration":true,"noEmitOnError":true`, 1)
			if changed == text {
				tb.Fatal("producer configuration did not enable noEmitOnError")
			}
			return changed
		}
		text, ok := f.fs.ReadFile(name)
		if !ok {
			tb.Fatal("missing producer configuration")
		}
		if err := f.fs.WriteFile(name, withNoEmitOnError(text)); err != nil {
			tb.Fatal(err)
		}
		if change.fileName == name {
			for i := range change.text {
				change.text[i] = withNoEmitOnError(change.text[i])
			}
		}
	}
	// Automatic generation resolves the live configuration through the Session.
	// The fixture's pre-parsed producer config is used only by prepared emit.
	return f, change
}

func checkEditorDeclarationGenerationOptions(tb testing.TB, s *Session, noEmitOnError bool) {
	tb.Helper()
	reference := s.Snapshot().GetDefaultProject(editorBoundaryURI).Program.GetProjectReferenceFromSource("/producer/src/module0.ts")
	if reference == nil || reference.Resolved.CompilerOptions().NoEmitOnError.IsTrue() != noEmitOnError {
		tb.Fatal("resolved producer noEmitOnError option does not match the benchmark arm")
	}
}

func benchmarkEditorDeclarationFreshSession(b *testing.B, f editorBoundaryFixture, declarations, noEmitOnError bool) {
	s := f.open()
	if len(editorBoundaryDiagnostics(b, s)) != 1 {
		b.Fatal("fresh Session did not report the expected consumer diagnostic")
	}
	editorBoundaryDependencies(b, s, declarations)
	checkEditorDeclarationGenerationOptions(b, s, noEmitOnError)
	s.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s = f.open()
		editorBoundaryDiagnostics(b, s)
		s.Close()
	}
}

// FreshSession includes opening, initial checking, automatic generation when
// selected, background draining and closing a Session. Fixture construction is
// excluded. This is fresh Session state, not cold process or operating-system
// caches. Producer edits include complete invalidation and generation work.
func BenchmarkEditorDeclarationGeneration(b *testing.B) {
	b.Run("SourceDependencies", func(b *testing.B) {
		b.Run("FreshSession", func(b *testing.B) {
			benchmarkEditorDeclarationFreshSession(b, newEditorBoundaryFixture(b, false), false, false)
		})
	})
	for _, noEmitOnError := range []bool{false, true} {
		name := "Automatic"
		if noEmitOnError {
			name = "AutomaticNoEmitOnError"
		}
		b.Run(name, func(b *testing.B) {
			for _, kind := range []string{"FreshSession", "BodyOnly", "PublicSignature"} {
				b.Run(kind, func(b *testing.B) {
					changeKind := kind
					if kind == "FreshSession" {
						changeKind = ""
					}
					f, change := editorDeclarationGenerationFixture(b, noEmitOnError, changeKind)
					if kind == "FreshSession" {
						benchmarkEditorDeclarationFreshSession(b, f, true, noEmitOnError)
						return
					}
					s := f.open()
					defer s.Close()
					editorBoundaryDependencies(b, s, true)
					checkEditorDeclarationGenerationOptions(b, s, noEmitOnError)
					wantChanged := 1
					if kind == "PublicSignature" {
						wantChanged = 2
					}
					if len(changeEditorBoundaryProducer(b, f, s, change, 1)) != wantChanged ||
						len(changeEditorBoundaryProducer(b, f, s, change, 0)) != 1 {
						b.Fatal("producer edit preflight did not update and restore consumer diagnostics")
					}
					state := 1
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						changeEditorBoundaryProducer(b, f, s, change, state)
						state = 1 - state
					}
				})
			}
		})
	}
}

func warmEditorDeclarationGeneration(tb testing.TB, noEmitOnError bool) {
	f, _ := editorDeclarationGenerationFixture(tb, noEmitOnError, "")
	s := f.open()
	editorBoundaryDiagnostics(tb, s)
	checkEditorDeclarationGenerationOptions(tb, s, noEmitOnError)
	s.Close()
}

// Run only with -benchtime=1x. Each sample retains two Sessions after 16 or 128
// distinct, fixed-width body revisions. Historical strings and snapshots are
// not retained by the harness. Current output, input tracking and ordinary
// checker/parse-cache ownership remain included in the post-GC live heap.
func BenchmarkEditorDeclarationGenerationRetainedHeap(b *testing.B) {
	for _, noEmitOnError := range []bool{false, true} {
		name := "Automatic"
		if noEmitOnError {
			name = "AutomaticNoEmitOnError"
		}
		b.Run(name, func(b *testing.B) {
			for _, edits := range []int{16, 128} {
				b.Run(fmt.Sprintf("Edits_%d", edits), func(b *testing.B) {
					if b.N != 1 {
						b.Skip("retained-heap sampling requires -benchtime=1x")
					}
					warmEditorDeclarationGeneration(b, noEmitOnError)
					f, _ := editorDeclarationGenerationFixture(b, noEmitOnError, "")
					const fileName = "/producer/src/module0.ts"
					baseText, ok := f.fs.ReadFile(fileName)
					if !ok {
						b.Fatal("missing producer source")
					}
					const batchSize = 2
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
					var firstSessionFinished time.Time
					runtime.ReadMemStats(&before)
					b.ResetTimer()
					for i := range sessions {
						if err := f.fs.WriteFile(fileName, baseText); err != nil {
							b.Fatal(err)
						}
						sessions[i] = f.open()
						initialDeclaration, ok := sessions[i].Snapshot().GetDefaultProject(editorBoundaryURI).Program.GetGeneratedDeclarationText("/producer/types/module0.d.ts")
						if !ok {
							b.Fatal("initial declaration was not generated")
						}
						for version := 1; version <= edits; version++ {
							change := editorBoundaryProducerChange{fileName: fileName}
							change.text[0] = strings.Replace(baseText, "  return local255;", fmt.Sprintf("  // revision %08d\n  return local255;", version), 1)
							if len(changeEditorBoundaryProducer(b, f, sessions[i], change, 0)) != 1 {
								b.Fatal("body revision changed consumer diagnostics")
							}
						}
						if text, ok := sessions[i].Snapshot().GetDefaultProject(editorBoundaryURI).Program.GetGeneratedDeclarationText("/producer/types/module0.d.ts"); !ok || text != initialDeclaration {
							b.Fatal("body revisions changed the public declaration text")
						}
						editorBoundaryDependencies(b, sessions[i], true)
						if sessions[i].parseCache.entries.Size() != editorBoundaryModules+3 {
							b.Fatal("historical parse-cache entries accumulated")
						}
						if i == 0 {
							firstSessionFinished = time.Now()
						}
					}
					b.StopTimer()
					runtime.GC()
					runtime.GC()
					runtime.ReadMemStats(&after)
					idle := time.Since(firstSessionFinished)
					if idle >= 29*time.Second {
						b.Skip("retention batch approached the default 30-second checker idle eviction")
					}
					runtime.KeepAlive(f)
					runtime.KeepAlive(baseText)
					runtime.KeepAlive(sessions)
					live := float64(int64(after.HeapAlloc) - int64(before.HeapAlloc))
					b.ReportMetric(live, "live-B/batch")
					b.ReportMetric(live/batchSize, "live-B/session")
					b.ReportMetric(batchSize, "sessions/batch")
					b.ReportMetric(float64(edits), "unique-edits/session")
					b.ReportMetric(editorBoundaryModules+3, "cache-entries/session")
					b.ReportMetric(float64(idle)/float64(time.Millisecond), "oldest-session-idle-ms")
				})
			}
		})
	}
}
