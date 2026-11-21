package output

import (
	"fmt"
	"path/filepath"
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