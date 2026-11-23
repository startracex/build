// Package filepath replaces path/filepath for path manipulation that a vfs.FS
// understands.
//
// path/filepath follows the host: it separates with a backslash on Windows, and
// it treats a drive letter as the only absolute root. A virtual file system uses
// slash-separated rooted paths, and a wasm build has no Windows path rules at
// all, so a host-specific join or absolute check points at the wrong file. This
// package delegates to tsgo's tspath, which applies one set of rules on every
// platform and always produces slash-separated paths.
//
// The API mirrors the part of path/filepath the build uses, so a call site only
// changes its import. A path that leaves the build therefore stays in slash
// form; os.Open and os.MkdirAll accept that form on Windows as well.
package filepath

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/startracex/tsgo/pkg/tspath"
)

// Separator is the path separator of this package, the same on every platform.
const Separator = '/'

// ToSlash returns path with every separator replaced by a slash.
func ToSlash(path string) string {
	return tspath.NormalizeSlashes(path)
}

// FromSlash returns path with separators normalized to slashes. Unlike
// path/filepath it does not produce a backslash on Windows, because a vfs path
// is always slash separated.
func FromSlash(path string) string {
	return tspath.NormalizeSlashes(path)
}

// IsAbs reports whether path is rooted, either with a slash or with a drive
// letter.
func IsAbs(path string) bool {
	return tspath.PathIsAbsolute(path)
}

// Base returns the last element of path.
func Base(path string) string {
	return tspath.GetBaseFileName(path)
}

// Dir returns all but the last element of path, or an empty string when path has
// no directory. Unlike path/filepath it does not return ".".
func Dir(path string) string {
	return tspath.GetDirectoryPath(path)
}

// Ext returns the extension of path, including the leading dot, or an empty
// string when path has none.
func Ext(path string) string {
	return tspath.GetAnyExtensionFromPath(path, nil, tspath.CaseSensitive)
}

// Join joins elements with slashes and cleans the result. Elements before the
// first non-empty, non-"." element drop out, so Join(".", "a.js") is "a.js" and
// Join("dist", "a.js") is "dist/a.js".
func Join(elem ...string) string {
	parts := make([]string, 0, len(elem))
	for _, part := range elem {
		if part == "" {
			continue
		}
		parts = append(parts, ToSlash(part))
	}
	for len(parts) > 1 && (parts[0] == "." || parts[0] == "") {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && parts[0] == "." {
		return "."
	}
	return tspath.CombinePaths(parts[0], parts[1:]...)
}

// Rel returns target relative to base. Both paths must be rooted or both
// relative, as with path/filepath.
func Rel(base, target string) (string, error) {
	if IsAbs(base) != IsAbs(target) {
		return "", errors.New("filepath: paths must either both be absolute or both be relative")
	}
	if base == "" {
		return "", errors.New("filepath: Rel: can't make " + target + " relative to " + base)
	}
	rel := tspath.GetRelativePathFromDirectory(ToSlash(base), ToSlash(target), tspath.CaseSensitive)
	if rel == "" {
		// tspath reports an empty string for the same path; a relative path to
		// itself is "."
		return ".", nil
	}
	return rel, nil
}

// Abs resolves path against the process working directory. A build that must not
// read the host working directory passes its own root to AbsIn instead.
func Abs(path string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return AbsIn(ToSlash(cwd), path)
}

// AbsIn resolves path against cwd, which is the root a build was given rather
// than the host working directory. An absolute path is only cleaned.
func AbsIn(cwd, path string) (string, error) {
	p := ToSlash(path)
	if p == "" {
		return "", errors.New("filepath: empty path")
	}
	if IsAbs(p) {
		return tspath.NormalizePath(p), nil
	}
	if cwd == "" {
		return Abs(p)
	}
	return tspath.CombinePaths(ToSlash(cwd), p), nil
}

// Clean returns the shortest path name equivalent to path.
func Clean(path string) string {
	return tspath.NormalizePath(ToSlash(path))
}

// Native converts a slash-separated path to the separator of the host. Every
// path inside a build is slash separated; a path handed to os or returned to a
// caller is the one place the host layout applies. On a wasm target, which has
// no backslash, this is a no-op.
func Native(path string) string {
	return filepath.FromSlash(path)
}

// EvalSymlinks reports the target of a symbolic link. Links exist on a real file
// system only, so this stays on the host implementation; a virtual file system
// answers the same question through vfs.FS.Realpath.
func EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
