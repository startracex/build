package modpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleID returns a slash-normalized module identifier for an absolute path.
func ModuleID(path string) string {
	return filepath.ToSlash(path)
}

// AbsFile resolves path to a clean absolute path and ensures it is a file.
func AbsFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
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
