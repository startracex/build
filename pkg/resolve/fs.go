package resolve

import (
	"sync"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// realpathFS retries a probe through the target of the path when the first
// attempt fails. Windows refuses to traverse an untrusted mount point, and a
// package manager can leave dependency directories as reparse points that fail
// that check, while the link target itself stays readable.
//
// A base FS that is not the OS file system does its own resolution: there is no
// native path to walk, so Realpath answers instead.
type realpathFS struct {
	vfs.FS
	native bool
	cache  sync.Map // rooted path string -> tspath.RootedPath
}

func newRealpathFS(base vfs.FS, native bool) *realpathFS {
	return &realpathFS{FS: base, native: native}
}

// real reports the target of p and whether it differs from p.
func (f *realpathFS) real(p tspath.RootedPath) (tspath.RootedPath, bool) {
	key := p.AsString()
	if cached, ok := f.cache.Load(key); ok {
		target, _ := cached.(tspath.RootedPath)
		return target, target != p
	}
	target := p
	if !f.native {
		target = f.FS.Realpath(p)
		f.cache.Store(key, target)
		return target, target != p
	}
	// filepath.EvalSymlinks walks the links one component at a time, which
	// succeeds where opening a handle to the whole path does not.
	if real, err := filepath.EvalSymlinks(filepath.FromSlash(key)); err == nil && real != "" {
		target = tspath.RootedPathFromAbsolute(filepath.ToSlash(real))
	}
	f.cache.Store(key, target)
	return target, target != p
}

// realNative returns the target of a native absolute path, or the path itself
// when it does not resolve.
func (f *realpathFS) realNative(path string) string {
	rooted, ok := tspath.TryRootedPathFromAbsolute(filepath.ToSlash(path))
	if !ok {
		return path
	}
	target, changed := f.real(rooted)
	if !changed {
		return path
	}
	return filepath.FromSlash(target.AsString())
}

func (f *realpathFS) FileExists(path tspath.RootedFilePath) bool {
	if f.FS.FileExists(path) {
		return true
	}
	if target, changed := f.real(tspath.RootedPath(path)); changed {
		return f.FS.FileExists(tspath.RootedFilePathFromPath(target))
	}
	return false
}

func (f *realpathFS) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	if f.FS.DirectoryExists(path) {
		return true
	}
	if target, changed := f.real(tspath.RootedPath(path)); changed {
		return f.FS.DirectoryExists(tspath.RootedDirectoryPathFromPath(target))
	}
	return false
}

func (f *realpathFS) ReadFile(path tspath.RootedFilePath) (string, bool) {
	if contents, ok := f.FS.ReadFile(path); ok {
		return contents, true
	}
	if target, changed := f.real(tspath.RootedPath(path)); changed {
		return f.FS.ReadFile(tspath.RootedFilePathFromPath(target))
	}
	return "", false
}

func (f *realpathFS) Stat(path tspath.RootedPath) vfs.FileInfo {
	if info := f.FS.Stat(path); info != nil {
		return info
	}
	if target, changed := f.real(path); changed {
		return f.FS.Stat(target)
	}
	return nil
}

func (f *realpathFS) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	entries := f.FS.GetAccessibleEntries(path)
	if len(entries.Files) > 0 || len(entries.Directories) > 0 {
		return entries
	}
	if target, changed := f.real(tspath.RootedPath(path)); changed {
		return f.FS.GetAccessibleEntries(tspath.RootedDirectoryPathFromPath(target))
	}
	return entries
}
