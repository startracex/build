package output

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
)

const (
	FormatESM  = "esm"
	FormatCJS  = "cjs"
	FormatIIFE = "iife"
	FormatUMD  = "umd"
)

// DeriveOutFile picks an extension for dual-format outfile emission; the result uses the host separator.
func DeriveOutFile(base string, format string) string {
	dir := filepath.Dir(base)
	stem := modpath.BaseStem(base)
	switch format {
	case FormatCJS:
		return filepath.Native(filepath.Join(dir, stem+".cjs"))
	case FormatIIFE, FormatUMD:
		return filepath.Native(filepath.Join(dir, stem+".js"))
	default:
		ext := modpath.LowerExt(base)
		if ext == ".cjs" || ext == ".js" {
			return filepath.Native(filepath.Join(dir, stem+".mjs"))
		}
		if ext == ".mjs" {
			return base
		}
		return filepath.Native(filepath.Join(dir, stem+".mjs"))
	}
}

// DeriveOutDir nests format under a shared outdir for multi-format lib builds; the result uses the host separator.
func DeriveOutDir(base string, format string) string {
	return filepath.Native(filepath.Join(base, string(format)))
}

// DefaultHashLength is used by [hash] when no length is specified.
const DefaultHashLength = 8

var hashPlaceholder = regexp.MustCompile(`\[hash(?::(\d+))?\]`)

// HasHashPlaceholder reports whether pattern contains a well-formed [hash] or [hash:N] placeholder.
func HasHashPlaceholder(pattern string) bool {
	return hashPlaceholder.MatchString(pattern)
}

// ContentHash returns the SHA-256 digest used by [hash] placeholders.
func ContentHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// FileNameParts holds the values a filename pattern can interpolate:
//
//	[name]     base name without extension
//	[path]     source-relative path without extension
//	[ext]      extension without the leading dot
//	[type]     file kind: entry, chunk, or asset
//	[format]   output format (esm, cjs, iife, umd)
//	[hash]     content digest, truncated to DefaultHashLength
//	[hash:N]   content digest truncated to N characters
type FileNameParts struct {
	Name   string
	Path   string
	Ext    string
	Type   string
	Format string
	Hash   string
}

// RenderFileName expands placeholders; an unset one becomes empty so optional parts work, and an empty Hash is derived from the rendered text to keep [hash] deterministic.
func RenderFileName(pattern string, parts FileNameParts) string {
	rendered := strings.NewReplacer(
		"[name]", parts.Name,
		"[path]", parts.Path,
		"[ext]", strings.TrimPrefix(parts.Ext, "."),
		"[type]", parts.Type,
		"[format]", parts.Format,
	).Replace(pattern)
	hash := parts.Hash
	if hash == "" {
		hash = ContentHash(rendered)
	}
	return hashPlaceholder.ReplaceAllStringFunc(rendered, func(match string) string {
		length := DefaultHashLength
		sub := hashPlaceholder.FindStringSubmatch(match)
		if len(sub) > 1 && sub[1] != "" {
			if n, err := strconv.Atoi(sub[1]); err == nil && n > 0 {
				length = n
			}
		}
		if length > len(hash) {
			length = len(hash)
		}
		return hash[:length]
	})
}

// RenderEntryFileName expands [name], [path], [format], [hash], and [hash:N]; it is RenderFileName without the [ext] inputs, for callers that only name entry files.
func RenderEntryFileName(pattern, name, path, format, hash string) string {
	return RenderFileName(pattern, FileNameParts{Name: name, Path: path, Format: format, Hash: hash})
}

// ValidateOutputPaths checks outfile/outdir constraints for one emit target.
func ValidateOutputPaths(outFile, outDir string) error {
	if outFile != "" && outDir != "" {
		return fmt.Errorf("outfile and outdir cannot both be set on one output")
	}
	return nil
}
