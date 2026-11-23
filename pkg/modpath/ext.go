package modpath

import (
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Ext returns the extension of path, including the leading dot. It returns an
// empty string when path has no extension.
func Ext(path string) string {
	return filepath.Ext(path)
}

// LowerExt returns the lowercase extension of path, including the leading dot.
func LowerExt(path string) string {
	return strings.ToLower(Ext(path))
}

// Stem returns path without its extension.
func Stem(path string) string {
	return strings.TrimSuffix(path, Ext(path))
}

// BaseStem returns the base name of path without its extension.
func BaseStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, Ext(base))
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

// IsTypeScriptSource reports whether path names a TypeScript file, including
// declaration files.
func IsTypeScriptSource(path string) bool {
	switch LowerExt(path) {
	case tspath.ExtensionTs, tspath.ExtensionTsx, tspath.ExtensionMts, tspath.ExtensionCts:
		return true
	default:
		return false
	}
}

// IsJavaScriptSource reports whether path names a JavaScript source file.
func IsJavaScriptSource(path string) bool {
	switch LowerExt(path) {
	case tspath.ExtensionJs, tspath.ExtensionJsx, tspath.ExtensionMjs, tspath.ExtensionCjs:
		return true
	default:
		return false
	}
}

// IsDeclaration reports whether path names a TypeScript declaration file:
// .d.ts, .d.mts or .d.cts.
func IsDeclaration(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, tspath.ExtensionDts) ||
		strings.HasSuffix(lower, tspath.ExtensionDmts) ||
		strings.HasSuffix(lower, tspath.ExtensionDcts)
}

// IsTypeScript reports whether path names a TypeScript file the build can
// compile: a TypeScript source file that is not a declaration file.
func IsTypeScript(path string) bool {
	return IsTypeScriptSource(path) && !IsDeclaration(path)
}

// IsJavaScriptOutput reports whether path names emitted JavaScript.
func IsJavaScriptOutput(path string) bool {
	switch LowerExt(path) {
	case tspath.ExtensionJs, tspath.ExtensionMjs, tspath.ExtensionCjs:
		return !IsDeclaration(path)
	default:
		return false
	}
}
