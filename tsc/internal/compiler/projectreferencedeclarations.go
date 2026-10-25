package compiler

import (
	"context"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type projectReferenceDeclarationHost struct {
	CompilerHost
	references map[tspath.PathKey]*tsoptions.ParsedCommandLine
}

func (h *projectReferenceDeclarationHost) GetResolvedProjectReference(fileName tspath.RootedFilePath, path tspath.PathKey) *tsoptions.ParsedCommandLine {
	if config, ok := h.references[path]; ok {
		return config
	}
	return h.CompilerHost.GetResolvedProjectReference(fileName, path)
}

func (p *fileLoader) prepareProjectReferenceDeclarations() {
	mapper := p.projectReferences.projectReferenceFileMapper
	active := make(map[tspath.PathKey]bool)
	acyclic := make(map[tspath.PathKey]bool)
	var visit func(tspath.PathKey) bool
	visit = func(path tspath.PathKey) bool {
		if active[path] {
			return false
		}
		if result, ok := acyclic[path]; ok {
			return result
		}
		active[path] = true
		result := true
		for _, reference := range mapper.referencesInConfigFile[path] {
			if !visit(reference) {
				result = false
				break
			}
		}
		delete(active, path)
		acyclic[path] = result
		return result
	}
	for path, config := range mapper.configToProjectReference {
		if config == nil || config.ConfigFile == nil {
			continue
		}
		options := config.CompilerOptions()
		if options.IsolatedDeclarations.IsTrue() && options.GetEmitDeclarations() && !options.NoEmit.IsTrue() &&
			len(config.ContentMapperExtensions()) == 0 && len(config.GetConfigFileParsingDiagnostics()) == 0 && visit(path) {
			mapper.declarationProjects.Add(config)
		}
	}
	for path, reference := range mapper.outputDtsToProjectReference {
		if mapper.declarationProjects.Has(reference.Resolved) {
			mapper.dtsDirectories.Add(path.Parent())
		}
	}
}

func (p *fileLoader) referenceFileExists(fileName tspath.RootedFilePath) bool {
	if p.projectReferences.declarationProjects.Len() != 0 {
		return p.projectReferences.host.FS().FileExists(fileName)
	}
	return p.host.FS().FileExists(fileName)
}

func (mapper *projectReferenceFileMapper) getGeneratedDeclarationText(path tspath.PathKey) (string, bool) {
	if text, ok := mapper.generatedDeclarations.Load(path); ok {
		return text, true
	}
	if reference := mapper.getProjectReferenceFromOutputDtsOrRealpath(path); reference != nil {
		return mapper.generatedDeclarations.Load(reference.OutputDtsPath)
	}
	if strings.HasSuffix(string(path), ".map") {
		if reference := mapper.getProjectReferenceFromOutputDtsOrRealpath(tspath.PathKey(strings.TrimSuffix(string(path), ".map"))); reference != nil {
			return mapper.generatedDeclarations.Load(tspath.PathKey(string(reference.OutputDtsPath) + ".map"))
		}
	}
	return "", false
}

func (p *fileLoader) generateProjectReferenceDeclaration(file ast.HasFileName) (string, bool) {
	reference := p.projectReferences.getProjectReferenceFromOutputDtsOrRealpath(file.PathKey())
	if reference == nil || !p.projectReferences.declarationProjects.Has(reference.Resolved) {
		return "", false
	}
	p.generateProjectReferenceDeclarations(reference.Resolved)
	return p.projectReferences.getGeneratedDeclarationText(file.PathKey())
}

func (p *fileLoader) generateProjectReferenceDeclarations(config *tsoptions.ParsedCommandLine) {
	once, _ := p.declarationEmits.LoadOrStore(config, new(sync.Once))
	once.Do(func() {
		// Generate referenced projects first, preserving the same declaration
		// boundary as an ordinary producer build. Eligibility excludes cycles.
		for _, path := range p.projectReferences.referencesInConfigFile[config.ConfigFile.SourceFile.PathKey()] {
			dependency := p.projectReferences.configToProjectReference[path]
			if !p.projectReferences.declarationProjects.Has(dependency) {
				continue
			}
			for _, output := range dependency.OutputDtsToProjectReference() {
				if !p.host.FS().FileExists(output.OutputDts) {
					p.generateProjectReferenceDeclarations(dependency)
					break
				}
			}
		}
		// Use a fresh host over the tracked consumer filesystem. Producer trees
		// must not acquire leases in the consumer's parse cache.
		base := &compilerResolutionHost{host: p.host, baseDirectory: config.BaseDirectory()}
		fs := p.projectReferences.resolutionHost(base, nil).FS()
		host := &projectReferenceDeclarationHost{
			CompilerHost: NewCompilerHost(fs, p.defaultLibraryPath, nil, p.host.Trace, nil),
			references:   p.projectReferences.configToProjectReference,
		}
		program := NewProgram(ProgramOptions{
			Config:         config,
			Host:           host,
			SingleThreaded: core.TSTrue,
			skipProjectReferenceDeclarationGeneration: true,
		})
		outputs := emitProjectReferenceDeclarations(program)
		// Publish only owned text after the entire producer emit succeeds.
		// Neither diagnostics nor producer AST/checker objects escape this scope.
		for fileName, text := range outputs {
			if !fileName.IsDeclarationFile() || p.host.FS().FileExists(fileName) {
				continue
			}
			path := p.caseSensitivity.PathKey(fileName.AsPath())
			owner := p.projectReferences.getProjectReferenceFromOutputDts(path)
			if owner == nil || owner.Resolved != config {
				continue
			}
			p.projectReferences.generatedDeclarations.Store(path, text)
			mapName := fileName.AppendSuffix(".map")
			if text, ok := outputs[mapName]; ok {
				p.projectReferences.generatedDeclarations.Store(p.caseSensitivity.PathKey(mapName.AsPath()), text)
			}
		}
	})
}

// Keep producer diagnostics and emit metadata inside this scope. Only owned
// declaration and map text can be retained by the consuming program.
func emitProjectReferenceDeclarations(program *Program) map[tspath.RootedFilePath]string {
	if !program.SingleThreaded() {
		panic("project-reference declaration capture requires a single-threaded program")
	}
	ctx := context.Background()
	if len(program.GetConfigFileParsingDiagnostics()) != 0 || len(program.GetProgramDiagnostics()) != 0 ||
		len(program.GetSyntacticDiagnostics(ctx, nil)) != 0 {
		return nil
	}
	outputs := make(map[tspath.RootedFilePath]string)
	result := program.Emit(ctx, EmitOptions{
		EmitOnly: EmitOnlyDts,
		WriteFile: func(path tspath.RootedFilePath, text string, _ *WriteFileData) error {
			outputs[path] = text
			return nil
		},
	})
	if result.EmitSkipped || len(result.Diagnostics) != 0 {
		return nil
	}
	return outputs
}
