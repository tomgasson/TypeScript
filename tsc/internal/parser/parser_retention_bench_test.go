package parser

import (
	"runtime"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Run explicitly with -benchtime=1x. The returned parser is kept reachable after
// two collections, so sync.Pool eviction cannot hide source text retained by its
// scanner. Only the parser survives: the source and AST are created after the
// baseline and discarded in a separate helper. This controlled ownership sample
// is not ordinary post-GC sync.Pool retention, peak heap, or process RSS. Timing
// includes fixture construction and parsing; use BenchmarkParse for throughput.
func BenchmarkReturnedParserScannerRetention(b *testing.B) {
	if b.N != 1 {
		b.Skip("returned-parser heap sampling requires -benchtime=1x")
	}
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	p := parseRetentionBenchmarkSource(b)
	b.StopTimer()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "live-B/returned-parser")
	runtime.KeepAlive(p)
}

// Prevent source/AST locals from remaining in the sampling caller's stack frame.
//
//go:noinline
func parseRetentionBenchmarkSource(b *testing.B) *Parser {
	const sourceBytes = 8 << 20
	const prefix = "/*"
	const suffix = "*/\nexport const value: number = 1;\n"
	source := prefix + strings.Repeat("x", sourceBytes-len(prefix)-len(suffix)) + suffix
	p := newParser()
	opts := ast.SourceFileParseOptions{FileName: "/src/retention.ts", PathKey: "/src/retention.ts"}
	p.initializeState(opts, source, core.ScriptKindTS)
	p.nextToken()
	file := p.parseSourceFileWorker()
	if len(file.Diagnostics()) != 0 {
		b.Fatal("retention fixture must parse without diagnostics")
	}
	putParser(p)
	return p
}
