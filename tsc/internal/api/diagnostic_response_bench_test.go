package api_test

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/api"
)

var diagnosticResponseBenchmarkCases = []struct {
	name        string
	sourceBytes int
}{
	{"Tiny64B", 64},
	{"Large1MiB", 1 << 20},
}

// Conversion of one already parsed diagnostic. Warmup includes its position and
// line maps; source construction and parsing are outside the timed operation.
func BenchmarkDiagnosticResponse(b *testing.B) {
	for _, c := range diagnosticResponseBenchmarkCases {
		b.Run(c.name, func(b *testing.B) {
			diagnostic := diagnosticResponseFixture(c.sourceBytes)
			api.NewDiagnosticResponse(diagnostic)
			b.ReportAllocs()
			b.ResetTimer()
			var response *api.DiagnosticResponse
			for b.Loop() {
				response = api.NewDiagnosticResponse(diagnostic)
			}
			b.ReportMetric(float64(c.sourceBytes), "source-B/response")
			b.ReportMetric(float64(len(diagnosticResponseContextLine)), "context-B/response")
			runtime.KeepAlive(response)
		})
	}
}

//go:noinline
func captureDiagnosticResponseForHeap(sourceBytes int) *api.DiagnosticResponse {
	return api.NewDiagnosticResponse(diagnosticResponseFixture(sourceBytes))
}

// Run separately with -benchtime=1x. Only responses survive the sample; each
// source string and AST is created after the baseline in a separate helper.
// Reports retained Go heap, not peak heap or RSS. Timing includes fixture
// construction and parsing; use BenchmarkDiagnosticResponse for conversion cost.
func BenchmarkDiagnosticResponseRetainedHeap(b *testing.B) {
	for _, c := range diagnosticResponseBenchmarkCases {
		b.Run(c.name, func(b *testing.B) {
			if b.N != 1 {
				b.Skip("retained-heap sampling requires -benchtime=1x")
			}
			const batchSize = 32
			captureDiagnosticResponseForHeap(c.sourceBytes)
			responses := make([]*api.DiagnosticResponse, batchSize)
			runtime.GC()
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ResetTimer()
			for i := range responses {
				responses[i] = captureDiagnosticResponseForHeap(c.sourceBytes)
			}
			b.StopTimer()
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&after)
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc))/batchSize, "live-B/response")
			b.ReportMetric(float64(c.sourceBytes), "source-B/response")
			b.ReportMetric(float64(len(diagnosticResponseContextLine)), "context-B/response")
			runtime.KeepAlive(responses)
		})
	}
}
