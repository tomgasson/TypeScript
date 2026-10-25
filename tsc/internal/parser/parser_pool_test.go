package parser

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"gotest.tools/v3/assert"
)

func TestParserPoolReleasesScannerSource(t *testing.T) {
	// Keep this test sequential: it inspects a returned parser before another
	// parse can acquire it from the shared pool.
	source := `/// <reference path="./types.d.ts" />
// @ts-check
/** Value documentation. */
export const value = 0xA_B;
// @ts-expect-error
value();
`
	opts := ast.SourceFileParseOptions{FileName: "/src/index.ts", PathKey: "/src/index.ts"}
	p := newParser()
	p.initializeState(opts, source, core.ScriptKindTS)
	p.nextToken()
	file := p.parseSourceFileWorker()
	assert.Equal(t, len(file.Diagnostics()), 0)
	assert.Equal(t, len(file.CommentDirectives), 1)
	pooledScanner := p.scanner
	assert.Equal(t, pooledScanner.Text(), source)
	putParser(p)
	assert.Equal(t, pooledScanner.Text(), "")
	assert.Equal(t, pooledScanner.TokenValue(), "")
	assert.Equal(t, len(pooledScanner.CommentDirectives()), 0)

	// A subsequent parse can reuse scanner storage without changing the first
	// result's source text, comment directives, or lazily parsed JSDoc.
	next := ParseSourceFile(opts, "// @ts-ignore\nexport const next = 0xC_D;", core.ScriptKindTS)
	assert.Equal(t, len(next.Diagnostics()), 0)
	assert.Equal(t, len(next.CommentDirectives), 1)
	assert.Equal(t, next.CommentDirectives[0].Kind, ast.CommentDirectiveKindIgnore)
	assert.Equal(t, file.Text(), source)
	assert.Equal(t, len(file.Statements.Nodes), 2)
	assert.Equal(t, len(file.ReferencedFiles), 1)
	assert.Equal(t, file.ReferencedFiles[0].FileName, "./types.d.ts")
	assert.Assert(t, file.CheckJsDirective != nil && file.CheckJsDirective.Enabled)
	assert.Equal(t, len(file.CommentDirectives), 1)
	assert.Equal(t, file.CommentDirectives[0].Kind, ast.CommentDirectiveKindExpectError)
	jsdoc := file.Statements.Nodes[0].JSDoc(file)
	assert.Equal(t, len(jsdoc), 1)
	assert.Equal(t, scanner.GetTextOfJSDocComment(jsdoc[0].AsJSDoc().Comment), "Value documentation.")
	invalid := ParseSourceFile(opts, `export const broken = "`, core.ScriptKindTS)
	assert.Assert(t, len(invalid.Diagnostics()) > 0, "reused scanners must report lexical errors")
}
