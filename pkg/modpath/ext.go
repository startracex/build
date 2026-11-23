package modpath

import (
	"path/filepath"
	"strings"
)

// SourceExtensions lists the extensions that can be loaded as source or as an
// asset: TypeScript, JavaScript and JSON.
var SourceExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".json"}

// Ext returns the extension of path, including the leading dot. It returns an
// empty string when path has no extension.
func Ext(path string) string {
	return filepath.Ext(path)
}

// LowerExt returns the lowercase extension of path, including the leading dot.
func LowerExt(path string) string {
	return strings.ToLower(filepath.Ext(path))
}

// Stem returns path without its extension.
func Stem(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path))
}

// BaseStem returns the base name of path without its extension.
func BaseStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ReplaceExt returns path with its extension replaced by ext. An empty ext
// drops the extension.
func ReplaceExt(path, ext string) string {
	return Stem(path) + ext
}

// HasExt reports whether path ends with ext, ignoring case. The extension may
// name more than one suffix, so ".d.ts" matches "index.d.ts" but not "index.ts".
func HasExt(path, ext string) bool {
	return strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext))
}

// HasAnyExt reports whether path ends with one of exts, ignoring case.
func HasAnyExt(path string, exts ...string) bool {
	lower := strings.ToLower(path)
	for _, ext := range exts {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// IsTypeScriptSource reports whether path names a TypeScript source file.
func IsTypeScriptSource(path string) bool {
	switch LowerExt(path) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return false
	}
}

// IsJavaScriptSource reports whether path names a JavaScript source file.
func IsJavaScriptSource(path string) bool {
	switch LowerExt(path) {
	case ".js", ".jsx", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

// IsSourcePath reports whether path names a file that can be loaded as source
// or as an asset.
func IsSourcePath(path string) bool {
	return HasAnyExt(path, SourceExtensions...)
}

// IsDeclarationPath reports whether path names a TypeScript declaration file.
func IsDeclarationPath(path string) bool {
	return HasExt(path, ".d.ts")
}

// IsJSONPath reports whether path names a JSON file.
func IsJSONPath(path string) bool {
	return LowerExt(path) == ".json"
}

// IsHTMLPath reports whether path names an HTML file.
func IsHTMLPath(path string) bool {
	return LowerExt(path) == ".html"
}
