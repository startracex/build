package output

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	FormatESM  = "esm"
	FormatCJS  = "cjs"
	FormatIIFE = "iife"
	FormatUMD  = "umd"
)

const (
	ModeApp = "app"
	ModeLib = "lib"
)

// DeriveOutFile picks an extension for dual-format outfile emission.
func DeriveOutFile(base string, format string) string {
	dir := filepath.Dir(base)
	stem := strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
	switch format {
	case FormatCJS:
		return filepath.Join(dir, stem+".cjs")
	case FormatIIFE, FormatUMD:
		return filepath.Join(dir, stem+".js")
	default:
		ext := strings.ToLower(filepath.Ext(base))
		if ext == ".cjs" || ext == ".js" {
			return filepath.Join(dir, stem+".mjs")
		}
		if ext == ".mjs" {
			return base
		}
		return filepath.Join(dir, stem+".mjs")
	}
}

// DeriveOutDir nests format under a shared outdir for multi-format lib builds.
func DeriveOutDir(base string, format string) string {
	return filepath.Join(base, string(format))
}

// DefaultHashLength is used by [hash] when no length is specified.
const DefaultHashLength = 8

var hashPlaceholder = regexp.MustCompile(`\[hash(?::(\d+))?\]`)

// HasHashPlaceholder reports whether pattern contains a well-formed [hash]
// or [hash:N] placeholder.
func HasHashPlaceholder(pattern string) bool {
	return hashPlaceholder.MatchString(pattern)
}

// ContentHash returns the SHA-256 digest used by [hash] placeholders.
func ContentHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// RenderEntryFileName expands [name], [format], [hash], and [hash:N] in an
// entry filename pattern. hash is usually the digest of the emitted content.
func RenderEntryFileName(pattern, name, format, hash string) string {
	rendered := strings.NewReplacer("[name]", name, "[format]", format).Replace(pattern)
	if hash == "" {
		hash = ContentHash(rendered)
	}
	return hashPlaceholder.ReplaceAllStringFunc(rendered, func(match string) string {
		length := DefaultHashLength
		parts := hashPlaceholder.FindStringSubmatch(match)
		if len(parts) > 1 && parts[1] != "" {
			if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
				length = n
			}
		}
		if length > len(hash) {
			length = len(hash)
		}
		return hash[:length]
	})
}

// ValidateOutputPaths checks outfile/outdir constraints for one emit target.
func ValidateOutputPaths(format, outFile, outDir, mode string, entryCount int) error {
	if outFile != "" && outDir != "" {
		return fmt.Errorf("outfile and outdir cannot both be set on one output")
	}
	if mode == ModeApp || mode == "" {
		if outFile != "" && entryCount != 1 {
			return fmt.Errorf("single-file output requires one entry")
		}
	}
	return nil
}
