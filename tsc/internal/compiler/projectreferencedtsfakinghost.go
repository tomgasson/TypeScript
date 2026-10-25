package compiler

import (
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/symlinks"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/cachedvfs"
)

type projectReferenceDtsFakingHost struct {
	currentDirectory tspath.RootedDirectoryPath
	fs               *cachedvfs.FS
}

var _ module.ResolutionHost = (*projectReferenceDtsFakingHost)(nil)

func newProjectReferenceDtsFakingHost(host module.ResolutionHost, references *projectReferenceFileMapper, generateDeclaration func(ast.HasFileName) (string, bool)) module.ResolutionHost {
	// Create a new host that will fake the dts files
	return &projectReferenceDtsFakingHost{
		currentDirectory: host.GetCurrentDirectory(),
		fs: cachedvfs.From(&projectReferenceDtsFakingVfs{
			host:                       host,
			projectReferenceFileMapper: references,
			knownSymlinks:              symlinks.KnownSymlinks{},
			generateDeclaration:        generateDeclaration,
		}),
	}
}

// FS implements module.ResolutionHost.
func (h *projectReferenceDtsFakingHost) FS() vfs.FS {
	return h.fs
}

// GetCurrentDirectory implements module.ResolutionHost.
func (h *projectReferenceDtsFakingHost) GetCurrentDirectory() tspath.RootedDirectoryPath {
	return h.currentDirectory
}

type projectReferenceDtsFakingVfs struct {
	host                       module.ResolutionHost
	projectReferenceFileMapper *projectReferenceFileMapper
	knownSymlinks              symlinks.KnownSymlinks
	// Set only while loading a program. Later resolvers can read successful output only.
	generateDeclaration func(ast.HasFileName) (string, bool)
}

var _ vfs.FS = (*projectReferenceDtsFakingVfs)(nil)

// CaseSensitivity implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) CaseSensitivity() tspath.CaseSensitivity {
	return fs.host.FS().CaseSensitivity()
}

// FileExists implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) FileExists(path tspath.RootedFilePath) bool {
	if fs.host.FS().FileExists(path) {
		return true
	}
	if _, ok := fs.getGeneratedDeclarationText(path); ok {
		return true
	}
	if !path.IsDeclarationFile() {
		return false
	}
	// Project references go to source file instead of .d.ts file
	return fs.fileExistsUsingSource(path)
}

// ReadFile implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) ReadFile(path tspath.RootedFilePath) (contents string, ok bool) {
	// A generated declaration's matching map supersedes a stale map left on disk.
	if text, ok := fs.getGeneratedDeclarationText(path); ok {
		return text, true
	}
	if contents, ok := fs.host.FS().ReadFile(path); ok {
		return contents, true
	}
	if fs.host.FS().FileExists(path) {
		return "", false
	}
	return fs.projectReferenceFileMapper.getGeneratedDeclarationText(fs.pathKey(path.AsPath()))
}

func (fs *projectReferenceDtsFakingVfs) getGeneratedDeclarationText(file tspath.RootedFilePath) (string, bool) {
	if text, ok := fs.projectReferenceFileMapper.getGeneratedDeclarationText(fs.pathKey(file.AsPath())); ok {
		return text, true
	}
	declaration, suffix := file, ""
	if name, isMap := strings.CutSuffix(file.AsString(), ".map"); isMap {
		declaration, suffix = tspath.RootedFilePathFromNormalized(name), ".map"
	}
	if !declaration.IsDeclarationFile() {
		return "", false
	}
	if fs.host.FS().FileExists(declaration) {
		return "", false
	}
	// A map or a direct read may be the first lookup through this package alias.
	fs.fileExistsUsingSource(declaration)
	if text, ok := fs.projectReferenceFileMapper.getGeneratedDeclarationText(fs.pathKey(file.AsPath())); ok {
		return text, true
	}
	if target, ok := fs.knownSymlinks.Files().Load(fs.pathKey(declaration.AsPath())); ok {
		return fs.projectReferenceFileMapper.getGeneratedDeclarationText(fs.pathKey(target.AppendSuffix(suffix).AsPath()))
	}
	return "", false
}

// WriteFile implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) WriteFile(path tspath.RootedFilePath, data string) error {
	panic("should not be called by resolver")
}

// AppendFile implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) AppendFile(path tspath.RootedFilePath, data string) error {
	panic("should not be called by resolver")
}

// Remove implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) Remove(path tspath.RootedPath) error {
	panic("should not be called by resolver")
}

// Chtimes implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) Chtimes(path tspath.RootedPath, aTime time.Time, mTime time.Time) error {
	panic("should not be called by resolver")
}

// DirectoryExists implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	if fs.host.FS().DirectoryExists(path) {
		fs.handleDirectoryCouldBeSymlink(path)
		return true
	}
	return fs.directoryExistsUsingSource(path)
}

// GetAccessibleEntries implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	return fs.host.FS().GetAccessibleEntries(path)
}

// Stat implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) Stat(path tspath.RootedPath) vfs.FileInfo {
	return fs.host.FS().Stat(path)
}

// Realpath implements vfs.FS.
func (fs *projectReferenceDtsFakingVfs) Realpath(path tspath.RootedPath) tspath.RootedPath {
	result, ok := fs.knownSymlinks.Files().Load(fs.pathKey(path))
	if ok {
		return result.AsPath()
	}
	return fs.host.FS().Realpath(path)
}

func (fs *projectReferenceDtsFakingVfs) pathKey(path tspath.RootedPath) tspath.PathKey {
	return fs.CaseSensitivity().PathKey(path)
}

func (fs *projectReferenceDtsFakingVfs) handleDirectoryCouldBeSymlink(directory tspath.RootedDirectoryPath) {
	if tspath.ContainsIgnoredDirectory(directory) {
		return
	}

	// Because we already watch node_modules, handle symlinks in there
	if !directory.ContainsLowercaseDirectorySequence("/node_modules/") {
		return
	}

	directoryPath := fs.pathKey(directory.AsPath())
	if _, ok := fs.knownSymlinks.Directories().Load(directoryPath); ok {
		return
	}

	realDirectory := tspath.RootedDirectoryPathFromPath(fs.Realpath(directory.AsPath()))
	if realDirectory == directory {
		// not symlinked
		return
	}
	realPath := fs.pathKey(realDirectory.AsPath())
	if realPath == directoryPath {
		// not symlinked
		return
	}
	fs.knownSymlinks.SetDirectory(directory, directoryPath, &symlinks.KnownDirectoryLink{
		Real:     realDirectory,
		RealPath: realPath,
	})
}

func (fs *projectReferenceDtsFakingVfs) fileExistsUsingSource(file tspath.RootedFilePath) bool {
	fileOrDirectory := tspath.RootedPath(file)
	filePath := fs.pathKey(file.AsPath())
	return fs.fileOrDirectoryExistsUsingSource(
		fileOrDirectory,
		func(path tspath.RootedPath) core.Tristate {
			return fs.fileExistsIfProjectReferenceDts(tspath.RootedFilePathFromPath(path))
		},
		module.NodeModulePackageRootForFile(file),
		func(realFile tspath.RootedFilePath) {
			fs.knownSymlinks.SetFile(file, filePath, realFile)
		},
	)
}

func (fs *projectReferenceDtsFakingVfs) directoryExistsUsingSource(directory tspath.RootedDirectoryPath) bool {
	return fs.fileOrDirectoryExistsUsingSource(
		tspath.RootedPath(directory),
		func(path tspath.RootedPath) core.Tristate {
			return fs.directoryExistsIfProjectReferenceDeclDir(tspath.RootedDirectoryPathFromPath(path))
		},
		module.NodeModulePackageRootForDirectory(directory),
		nil,
	)
}

func (fs *projectReferenceDtsFakingVfs) fileOrDirectoryExistsUsingSource(
	fileOrDirectory tspath.RootedPath,
	existsUsingSource func(tspath.RootedPath) core.Tristate,
	packageRoot tspath.RootedDirectoryPath,
	onFileExists func(tspath.RootedFilePath),
) bool {
	// Check current directory or file
	result := existsUsingSource(fileOrDirectory)
	if result != core.TSUnknown {
		return result == core.TSTrue
	}

	fileOrDirectoryPath := fs.pathKey(fileOrDirectory)
	if !fileOrDirectoryPath.ContainsLowercaseDirectorySequence("/node_modules/") {
		return false
	}
	// Check if the directory or file is a symlinked package
	if packageRoot != "" {
		fs.handleDirectoryCouldBeSymlink(packageRoot)
	}
	knownDirectoryLinks := fs.knownSymlinks.Directories()
	if knownDirectoryLinks.Size() == 0 {
		return false
	}
	if onFileExists != nil {
		_, ok := fs.knownSymlinks.Files().Load(fileOrDirectoryPath)
		if ok {
			return true
		}
	}

	// If it contains node_modules check if its one of the symlinked path we know of
	var exists bool
	knownDirectoryLinks.Range(func(directoryPath tspath.PathKey, knownDirectoryLink *symlinks.KnownDirectoryLink) bool {
		if directoryPath == fileOrDirectoryPath || !directoryPath.ContainsPath(fileOrDirectoryPath) {
			return true
		}
		realFileOrDirectory, ok := knownDirectoryLink.ResolveFilePath(tspath.RootedFilePathFromPath(fileOrDirectory), fs.CaseSensitivity())
		if !ok {
			panic("canonical symlink path did not match its presentation path")
		}
		if exists = existsUsingSource(tspath.RootedPath(realFileOrDirectory)).IsTrue(); exists {
			if onFileExists != nil {
				onFileExists(realFileOrDirectory)
			}
			return false
		}
		return true
	})
	return exists
}

func (fs *projectReferenceDtsFakingVfs) fileExistsIfProjectReferenceDts(file tspath.RootedFilePath) core.Tristate {
	source := fs.projectReferenceFileMapper.getProjectReferenceFromOutputDts(fs.pathKey(file.AsPath()))
	if source != nil {
		if !fs.projectReferenceFileMapper.useSourceOfProjectReference {
			if _, ok := fs.projectReferenceFileMapper.getGeneratedDeclarationText(fs.pathKey(file.AsPath())); ok {
				return core.TSTrue
			}
			if fs.generateDeclaration == nil || !fs.projectReferenceFileMapper.declarationProjects.Has(source.Resolved) {
				return core.TSFalse
			}
			_, ok := fs.generateDeclaration(ast.NewHasFileName(file, fs.pathKey(file.AsPath())))
			return core.IfElse(ok, core.TSTrue, core.TSFalse)
		}
		return core.IfElse(fs.host.FS().FileExists(source.Source), core.TSTrue, core.TSFalse)
	}
	return core.TSUnknown
}

func (fs *projectReferenceDtsFakingVfs) directoryExistsIfProjectReferenceDeclDir(dir tspath.RootedDirectoryPath) core.Tristate {
	dirPath := fs.pathKey(dir.AsPath())
	for declDirPath := range fs.projectReferenceFileMapper.dtsDirectories.Keys() {
		if dirPath.ContainsPath(declDirPath) || declDirPath.ContainsPath(dirPath) {
			return core.TSTrue
		}
	}
	return core.TSUnknown
}
