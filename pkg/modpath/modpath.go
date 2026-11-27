package modpath

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
)

// ModuleID returns a slash-normalized module identifier for an absolute path.
func ModuleID(path string) string {
	return filepath.ToSlash(path)
}

// NodeModulesRel returns the path of abs inside its package, using the innermost node_modules directory.
// The last match wins because package managers nest stores (node_modules/.pnpm/<pkg>@<version>/node_modules/<pkg>).
// Comparison ignores case (Windows/macOS) and drops query or fragment so a cache-busting suffix cannot reach an output name.
func NodeModulesRel(abs string) (string, bool) {
	if abs == "" {
		return "", false
	}
	slash := filepath.ToSlash(abs)
	if i := strings.IndexAny(slash, "?#"); i >= 0 {
		slash = slash[:i]
	}
	// Trimming the trailing separator avoids an empty last segment treating "node_modules/" as a package root.
	slash = strings.TrimRight(slash, "/")
	if slash == "" {
		return "", false
	}
	parts := strings.Split(slash, "/")
	for i, part := range slices.Backward(parts) {
		if !strings.EqualFold(part, "node_modules") {
			continue
		}
		if i == len(parts)-1 {
			return "", false
		}
		return strings.Join(parts[i+1:], "/"), true
	}
	return "", false
}

// AbsPath returns a clean absolute path, resolving relative paths against cwd so callers can supply the project root.
func AbsPath(cwd, path string) (string, error) {
	return filepath.AbsIn(cwd, path)
}

// AbsFile resolves path to a clean absolute path and asks fs whether it is a file, resolving relative paths against cwd.
// A nil FS reads the OS file system.
func AbsFile(fs vfs.FS, cwd, path string) (string, error) {
	abs, err := AbsPath(cwd, path)
	if err != nil {
		return "", err
	}
	if fs == nil {
		fs = osvfs.FS()
	}
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(abs)
	if !ok {
		return "", fmt.Errorf("%s: %w", abs, vfs.ErrNotExist)
	}
	info := fs.Stat(tspath.RootedPath(rooted))
	if info == nil {
		return "", fmt.Errorf("%s: %w", abs, vfs.ErrNotExist)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	return abs, nil
}

// ReadFile reads path through fs; a missing file reports fs.ErrNotExist so callers do not depend on fs-specific error text.
// A nil FS reads the OS file system.
func ReadFile(fs vfs.FS, path string) ([]byte, error) {
	if fs == nil {
		return os.ReadFile(path)
	}
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(filepath.ToSlash(path))
	if !ok {
		return nil, fmt.Errorf("%s: %w", path, vfs.ErrNotExist)
	}
	text, ok := fs.ReadFile(rooted)
	if !ok {
		return nil, fmt.Errorf("%s: %w", path, vfs.ErrNotExist)
	}
	return []byte(text), nil
}

// DisplayRoot picks cwd as the display root when all paths lie below it, otherwise their deepest common directory.
func DisplayRoot(cwd string, paths []string) string {
	if cwd != "" && len(paths) > 0 {
		inside := true
		for _, p := range paths {
			if !tspath.ContainsPath(cwd, p, tspath.CaseSensitive) {
				inside = false
				break
			}
		}
		if inside {
			return cwd
		}
	}
	if len(paths) == 0 {
		return cwd
	}
	root := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		root = CommonDir(root, p)
	}
	return root
}

// CommonDir returns the deepest directory containing both paths.
func CommonDir(dir, path string) string {
	for {
		if dir == "" || tspath.ContainsPath(dir, path, tspath.CaseSensitive) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// DisplayRel returns path relative to root for emitted file names.
func DisplayRel(root, path string) string {
	if root == "" || filepath.IsAbs(root) != filepath.IsAbs(path) {
		return filepath.Base(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.Base(path)
	}
	return rel
}

// JavascriptOutputAbs maps a TypeScript (or JSON) source path to its .js output path.
func JavascriptOutputAbs(abs string) string {
	switch LowerExt(abs) {
	case ".ts", ".tsx", ".mts", ".cts", ".json":
		return ReplaceExt(abs, ".js")
	default:
		return abs
	}
}
