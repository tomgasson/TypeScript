package compiler

import (
	"context"
	"runtime"
	"testing"
	"weak"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

type capturedEmitMetadata struct {
	fileName tspath.RootedFilePath
	data     *WriteFileData
}

func captureEmitMetadata(t *testing.T) (weak.Pointer[ast.SourceFile], []capturedEmitMetadata) {
	t.Helper()
	fs := vfstest.FromMap(map[string]string{
		"/src/Input.ts": "export const value: number = 1;",
	}, tspath.CaseInsensitive)
	options := &core.CompilerOptions{
		NoLib:          core.TSTrue,
		Declaration:    core.TSTrue,
		DeclarationMap: core.TSTrue,
		SourceMap:      core.TSTrue,
		OutDir:         "/out",
	}
	p := NewProgram(ProgramOptions{
		Config: tsoptions.NewParsedCommandLine(options, []tspath.RootedFilePath{"/src/Input.ts"}, nil, "/src", fs.CaseSensitivity()),
		Host:   NewCompilerHost(fs, "/", nil, nil, nil),
	})
	file := p.GetSourceFile("/src/Input.ts")
	if file == nil {
		t.Fatal("input source file was not loaded")
	}
	var outputs []capturedEmitMetadata
	result := p.Emit(context.Background(), EmitOptions{
		WriteFile: func(fileName tspath.RootedFilePath, _ string, data *WriteFileData) error {
			outputs = append(outputs, capturedEmitMetadata{fileName: fileName, data: data})
			return nil
		},
	})
	if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected emit result: %+v", result)
	}
	return weak.Make(file), outputs
}

func TestEmitMetadataDoesNotRetainSourceFile(t *testing.T) {
	reference, outputs := captureEmitMetadata(t)
	runtime.GC()
	runtime.GC()
	if reference.Value() != nil {
		t.Fatal("retained emit metadata kept the input implementation tree alive")
	}

	wantFiles := map[tspath.RootedFilePath]bool{
		"/out/Input.js":       true,
		"/out/Input.js.map":   true,
		"/out/Input.d.ts":     true,
		"/out/Input.d.ts.map": true,
	}
	if len(outputs) != len(wantFiles) {
		t.Fatalf("received %d outputs, want %d", len(outputs), len(wantFiles))
	}
	for _, output := range outputs {
		if !wantFiles[output.fileName] {
			t.Errorf("unexpected or repeated output: %s", output.fileName)
		}
		delete(wantFiles, output.fileName)
		data := output.data
		if data == nil || data.SourceFile == nil {
			t.Fatalf("missing source identity for %s", output.fileName)
		}
		if data.SourceFile.FileName() != "/src/Input.ts" || data.SourceFile.PathKey() != "/src/input.ts" {
			t.Errorf("source identity for %s = (%s, %s), want (/src/Input.ts, /src/input.ts)", output.fileName, data.SourceFile.FileName(), data.SourceFile.PathKey())
		}
		if len(data.Diagnostics) != 0 {
			t.Errorf("unexpected diagnostics for %s: %v", output.fileName, data.Diagnostics)
		}
	}
	runtime.KeepAlive(outputs)
}
