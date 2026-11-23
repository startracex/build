package modpath

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ModuleID returns a slash-normalized module identifier for an absolute path.
func ModuleID(path string) string {
	return filepath.ToSlash(path)
}

// NodeModulesRel returns the path of abs inside its package, using the
// innermost node_modules directory. A package manager can nest its store
// (node_modules/.pnpm/<pkg>@<version>/node_modules/<pkg>/...), so the last match
// decides where the package root is.
//
// The comparison ignores case because Windows and macOS resolve a differently
// cased directory to the same place, and a query or fragment is dropped so a
// cache-busting suffix cannot end up in an output file name.
func NodeModulesRel(abs string) (string, bool) {
	if abs == "" {
		return "", false
	}
	slash := filepath.ToSlash(abs)
	if i := strings.IndexAny(slash, "?#"); i >= 0 {
		slash = slash[:i]
	}
	// A trailing separator would otherwise leave an empty last segment and make
	// "node_modules/" look like a package root with an empty relative path.
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

// AbsFile resolves path to a clean absolute path and ensures it is a file.
func AbsFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	return abs, nil
}

// DisplayRoot picks a filesystem root for relative display paths.
func DisplayRoot(paths []string) string {
	cwd, err := os.Getwd()
	if err == nil && len(paths) > 0 {
		inside := true
		for _, p := range paths {
			rel, relErr := filepath.Rel(cwd, p)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
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
		rel, err := filepath.Rel(dir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
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
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.ToSlash(filepath.Base(path))
	}
	return filepath.ToSlash(rel)
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
