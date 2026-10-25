package incremental

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"gotest.tools/v3/assert"
)

func TestDiagnosticSignatureFileIdentity(t *testing.T) {
	t.Parallel()
	parseFile := func(name tspath.RootedFilePath, path tspath.PathKey) *ast.SourceFile {
		return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: name, PathKey: path}, "", core.ScriptKindTS)
	}
	file := parseFile("/src/index.ts", "/src/index.ts")
	identity := ast.NewHasFileName(file.FileName(), file.PathKey())
	const sameFile = "\n(7,5): error2304: Cannot_find_name_0_2304\nvalue\n"
	for _, c := range []struct {
		name           string
		origin         ast.HasFileName
		diagnosticFile *ast.SourceFile
		want           string
	}{
		{"same AST", file, file, sameFile},
		{"identity without AST", identity, file, sameFile},
		{"another generation", identity, parseFile("/src/index.ts", "/src/index.ts"), sameFile},
		{"same canonical path", identity, parseFile("/SRC/INDEX.ts", "/src/index.ts"), sameFile},
		{"another file", identity, parseFile("/dep/helper.ts", "/dep/helper.ts"), "\n../dep/helper.ts(7,5): error2304: Cannot_find_name_0_2304\nvalue\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			diagnostic := ast.NewDiagnostic(c.diagnosticFile, core.NewTextRange(7, 12), diagnostics.Cannot_find_name_0, "value")
			var builder strings.Builder
			diagnosticToStringBuilder(diagnostic, c.origin, &builder)
			assert.Equal(t, builder.String(), c.want)

			const text = "export declare const value: number;\n"
			data := &compiler.WriteFileData{SourceMapUrlPos: -1, Diagnostics: []*ast.Diagnostic{diagnostic}}
			var s snapshot
			assert.Equal(t, s.computeSignatureWithDiagnostics(c.origin, text, data), ComputeHash(text+c.want, false))
		})
	}
}

func TestDiagnosticSignatureWithoutFile(t *testing.T) {
	t.Parallel()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/src/index.ts", PathKey: "/src/index.ts"}, "", core.ScriptKindTS)
	otherGeneration := parser.ParseSourceFile(file.ParseOptions(), "", core.ScriptKindTS)
	otherFile := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/dep/helper.ts", PathKey: "/dep/helper.ts"}, "", core.ScriptKindTS)
	identity := ast.NewHasFileName(file.FileName(), file.PathKey())
	global := ast.NewCompilerDiagnostic(diagnostics.Type_0_is_not_assignable_to_type_1, "left", "right")
	nested := ast.NewDiagnostic(file, core.NewTextRange(1, 5), diagnostics.Cannot_find_name_0, "value").
		SetMessageChain([]*ast.Diagnostic{nil, global}).
		SetRelatedInfo([]*ast.Diagnostic{
			nil,
			ast.NewDiagnostic(otherGeneration, core.NewTextRange(2, 4), diagnostics.X_0_is_declared_here, "value"),
			ast.NewCompilerDiagnostic(diagnostics.X_0_is_declared_here, "global"),
			ast.NewDiagnostic(otherFile, core.NewTextRange(3, 7), diagnostics.X_0_is_declared_here, "helper"),
		})
	const globalText = "\nerror2322: Type_0_is_not_assignable_to_type_1_2322\nleft\nright\n"
	for _, c := range []struct {
		name       string
		diagnostic *ast.Diagnostic
		want       string
	}{
		{"nil diagnostic", nil, ""},
		{"global diagnostic", global, globalText},
		{"message chain and related information", nested,
			"\n(1,4): error2304: Cannot_find_name_0_2304\nvalue\n" + globalText +
				"\n(2,2): message2728: _0_is_declared_here_2728\nvalue\n" +
				"\nmessage2728: _0_is_declared_here_2728\nglobal\n" +
				"\n../dep/helper.ts(3,4): message2728: _0_is_declared_here_2728\nhelper\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var builder strings.Builder
			diagnosticToStringBuilder(c.diagnostic, identity, &builder)
			assert.Equal(t, builder.String(), c.want)

			data := &compiler.WriteFileData{SourceMapUrlPos: -1, Diagnostics: []*ast.Diagnostic{c.diagnostic}}
			var s snapshot
			assert.Equal(t, s.computeSignatureWithDiagnostics(identity, "declaration", data), ComputeHash("declaration"+c.want, false))
		})
	}
}
