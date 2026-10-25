package api_test

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"

	"github.com/microsoft/TypeScript/tsc/internal/api"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/json"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"gotest.tools/v3/assert"
)

func TestDiagnosticResponseContextJSON(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		text       string
		start, end int
		want       api.DiagnosticResponse
	}{
		{
			name: "UTF8 source and UTF16 positions", text: "// header\nconst 名 = '😀';\n", start: 23, end: 27,
			want: api.DiagnosticResponse{
				Pos: 21, End: 23,
				StartPosition: &api.DiagnosticPositionResponse{Line: 1, Character: 11},
				EndPosition:   &api.DiagnosticPositionResponse{Line: 1, Character: 13},
				SourceLines:   []*api.DiagnosticSourceLineResponse{{Line: 1, Text: "const 名 = '😀';\n"}},
			},
		},
		{
			name: "CRLF preserved", text: "first\r\nsecond\r\nlast", start: 7, end: 13,
			want: api.DiagnosticResponse{
				Pos: 7, End: 13,
				StartPosition: &api.DiagnosticPositionResponse{Line: 1, Character: 0},
				EndPosition:   &api.DiagnosticPositionResponse{Line: 1, Character: 6},
				SourceLines:   []*api.DiagnosticSourceLineResponse{{Line: 1, Text: "second\r\n"}},
			},
		},
		{
			name: "long span truncation", text: "one\ntwo\nthree\nfour\nfive\nsix\nseven", start: 0, end: 33,
			want: api.DiagnosticResponse{
				Pos: 0, End: 33,
				StartPosition: &api.DiagnosticPositionResponse{Line: 0, Character: 0},
				EndPosition:   &api.DiagnosticPositionResponse{Line: 6, Character: 5},
				SourceLines: []*api.DiagnosticSourceLineResponse{
					{Line: 0, Text: "one\n"}, {Line: 1, Text: "two\n"}, {Line: 5, Text: "six\n"}, {Line: 6, Text: "seven"},
				},
			},
		},
		{
			name: "empty final line", text: "value\n", start: 6, end: 6,
			want: api.DiagnosticResponse{
				Pos: 6, End: 6,
				StartPosition: &api.DiagnosticPositionResponse{Line: 1, Character: 0},
				EndPosition:   &api.DiagnosticPositionResponse{Line: 1, Character: 0},
				SourceLines:   []*api.DiagnosticSourceLineResponse{{Line: 1, Text: ""}},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/source.ts", PathKey: "/source.ts"}, c.text, core.ScriptKindTS)
			diagnostic := ast.NewDiagnostic(file, core.NewTextRange(c.start, c.end), diagnostics.Expression_expected)
			got := api.NewDiagnosticResponse(diagnostic)
			c.want.FileName, c.want.Code, c.want.Category, c.want.Text = "/source.ts", 1109, diagnostics.CategoryError, "Expression expected."
			wantJSON, err := json.Marshal(&c.want)
			assert.NilError(t, err)
			gotJSON, err := json.Marshal(got)
			assert.NilError(t, err)
			assert.Equal(t, string(gotJSON), string(wantJSON))
		})
	}
}

const diagnosticResponseContextLine = "export const value: number = missing;\r\n"

func diagnosticResponseFixture(sourceBytes int) *ast.Diagnostic {
	prefix, suffix := diagnosticResponseContextLine+"/*", "*/\n"
	text := prefix + strings.Repeat("x", sourceBytes-len(prefix)-len(suffix)) + suffix
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/source.ts", PathKey: "/source.ts"}, text, core.ScriptKindTS)
	pos := strings.Index(text, "missing")
	return ast.NewDiagnostic(file, core.NewTextRange(pos, pos+len("missing")), diagnostics.Cannot_find_name_0, "missing")
}

//go:noinline
func captureDiagnosticResponseOwnership() (*api.DiagnosticResponse, weak.Pointer[ast.SourceFile], weak.Pointer[byte]) {
	diagnostic := diagnosticResponseFixture(1 << 20)
	file := diagnostic.File()
	return api.NewDiagnosticResponse(diagnostic), weak.Make(file), weak.Make(unsafe.StringData(file.Text()))
}

func TestDiagnosticResponseReleasesSourceText(t *testing.T) { //nolint:paralleltest
	response, file, sourceText := captureDiagnosticResponseOwnership()
	collected := false
	for range 20 {
		runtime.GC()
		if file.Value() == nil && sourceText.Value() == nil {
			collected = true
			break
		}
	}
	assert.Assert(t, collected, "a short diagnostic response must not retain its source AST or 1 MiB source text")
	assert.DeepEqual(t, response.SourceLines, []*api.DiagnosticSourceLineResponse{{Line: 0, Text: diagnosticResponseContextLine}})
	assert.Equal(t, response.Text, "Cannot find name 'missing'.")
	runtime.KeepAlive(response)
}
