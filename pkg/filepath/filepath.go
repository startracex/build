// Package filepath replaces path/filepath with slash-based paths a vfs.FS understands.
// Host rules (backslash separators, drive-letter roots) point at the wrong file in a virtual or wasm build, so it delegates to tspath for one rule set on every platform.
// The API mirrors the path/filepath subset the build uses; slash paths stay in slash form, which os.Open and os.MkdirAll accept on Windows too.
package filepath

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/startracex/tsgo/pkg/tspath"
)

// Separator is the path separator, the same on every platform.
const Separator = '/'

// ToSlash returns path with every separator replaced by a slash.
func ToSlash(path string) string {
	return tspath.NormalizeSlashes(path)
}

// FromSlash normalizes separators to slashes; unlike path/filepath it never emits a backslash because vfs paths are slash-separated.
func FromSlash(path string) string {
	return tspath.NormalizeSlashes(path)
}

// IsAbs reports whether path is rooted, with a slash or a drive letter.
func IsAbs(path string) bool {
	return tspath.PathIsAbsolute(path)
}

// Base returns the last element of path.
func Base(path string) string {
	return tspath.GetBaseFileName(path)
}

// Dir returns all but the last element, or "" when there is no directory; unlike path/filepath it does not return ".".
func Dir(path string) string {
	return tspath.GetDirectoryPath(path)
}

// Ext returns the extension including the leading dot, or "" when path has none.
func Ext(path string) string {
	return tspath.GetAnyExtensionFromPath(path, nil, tspath.CaseSensitive)
}

// Join joins elements with slashes and cleans the result.
// Leading empty or "." elements drop out, so Join(".", "a.js") is "a.js".
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

// Rel returns target relative to base; both paths must be rooted or both relative.
func Rel(base, target string) (string, error) {
	if IsAbs(base) != IsAbs(target) {
		return "", errors.New("filepath: paths must either both be absolute or both be relative")
	}
	if base == "" {
		return "", errors.New("filepath: Rel: can't make " + target + " relative to " + base)
	}
	rel := tspath.GetRelativePathFromDirectory(ToSlash(base), ToSlash(target), tspath.CaseSensitive)
	if rel == "" {
		// tspath returns "" for the same path, but a relative path to itself is ".".
		return ".", nil
	}
	return rel, nil
}

// Abs resolves path against the working directory; use AbsIn to supply a build root instead of the host cwd.
func Abs(path string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return AbsIn(ToSlash(cwd), path)
}

// AbsIn resolves path against cwd, the build root rather than the host cwd; absolute paths are only cleaned.
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

// Native converts a slash path to the host separator; os-bound or returned paths are the only place host layout applies.
// On wasm there is no backslash, so this is a no-op.
func Native(path string) string {
	return filepath.FromSlash(path)
}

// EvalSymlinks reports the target of a symbolic link, staying on the host since links exist only on a real file system.
// A virtual file system answers this through vfs.FS.Realpath instead.
func EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
