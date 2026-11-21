package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/tsgo/pkg/tspath"
)

// sourceExts is the order used when a specifier has no extension.
// TypeScript sources win over already-emitted JavaScript.
var sourceExts = []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}

func (b *bundler) resolve(importer, spec string) (resolved string, external bool, err error) {
	if b.opts.Resolve != nil {
		path, ok, hookErr := b.opts.Resolve(importer, spec)
		if hookErr != nil {
			return "", false, hookErr
		}
		if ok {
			abs, err := absFile(path)
			if err != nil {
				return "", false, fmt.Errorf("resolve %q from %s: %w", spec, importer, err)
			}
			return abs, false, nil
		}
	}
	if !tspath.IsExternalModuleNameRelative(spec) {
		return "", true, nil
	}
	abs, err := resolveRelative(importer, spec)
	if err != nil {
		return "", false, err
	}
	return abs, false, nil
}

func resolveRelative(importer, spec string) (string, error) {
	target := filepath.Clean(filepath.Join(filepath.Dir(importer), filepath.FromSlash(spec)))
	for _, candidate := range candidates(target) {
		abs, err := absFile(candidate)
		if err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf("cannot resolve %q imported by %s", spec, importer)
}

func candidates(target string) []string {
	ext := strings.ToLower(filepath.Ext(target))
	switch ext {
	case ".js":
		return []string{swapExt(target, ".ts"), swapExt(target, ".tsx"), target, swapExt(target, ".jsx")}
	case ".jsx":
		return []string{swapExt(target, ".tsx"), swapExt(target, ".ts"), target}
	case ".mjs":
		return []string{swapExt(target, ".mts"), swapExt(target, ".ts"), target}
	case ".cjs":
		return []string{swapExt(target, ".cts"), swapExt(target, ".ts"), target}
	case ".ts", ".tsx", ".mts", ".cts":
		return []string{target}
	case "":
		out := make([]string, 0, len(sourceExts)*2)
		for _, ext := range sourceExts {
			out = append(out, target+ext)
		}
		for _, ext := range sourceExts {
			out = append(out, filepath.Join(target, "index"+ext))
		}
		return out
	default:
		return []string{target}
	}
}

func swapExt(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ext
}

func absFile(path string) (string, error) {
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

func moduleID(path string) string {
	return filepath.ToSlash(path)
}
