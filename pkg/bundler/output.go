package bundler

import (
	"fmt"
	"path/filepath"
	"strings"
)

// OutputOptions configures one emit target, like a Rollup output entry.
type OutputOptions struct {
	Format  Format
	OutFile string
	OutDir  string
	Name    string // global name for IIFE/UMD
}

func (o Options) normalizedOutputs() ([]OutputOptions, error) {
	if len(o.Outputs) > 0 {
		var outs []OutputOptions
		for _, out := range o.Outputs {
			format, err := normalizeFormat(out.Format)
			if err != nil {
				return nil, err
			}
			out.Format = format
			if out.OutFile == "" && out.OutDir == "" {
				out.OutFile = o.OutFile
				out.OutDir = o.OutDir
			}
			if out.Name == "" {
				out.Name = o.GlobalName
			}
			outs = append(outs, out)
		}
		return outs, nil
	}
	format, err := normalizeFormat(o.Format)
	if err != nil {
		return nil, err
	}
	return []OutputOptions{{
		Format:  format,
		OutFile: o.OutFile,
		OutDir:  o.OutDir,
		Name:    o.GlobalName,
	}}, nil
}

func withOutput(opts Options, out OutputOptions) Options {
	opts.Format = out.Format
	opts.OutFile = out.OutFile
	opts.OutDir = out.OutDir
	opts.GlobalName = out.Name
	opts.Outputs = nil
	return opts
}

// DeriveOutFile picks an extension for dual-format outfile emission.
func DeriveOutFile(base string, format Format) string {
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
func DeriveOutDir(base string, format Format) string {
	return filepath.Join(base, string(format))
}

func validateOutputPaths(out OutputOptions, mode Mode, entryCount int) error {
	if out.OutFile != "" && out.OutDir != "" {
		return fmt.Errorf("outfile and outdir cannot both be set on one output")
	}
	if mode == ModeApp || mode == "" {
		if out.OutFile != "" && entryCount != 1 {
			return fmt.Errorf("single-file output requires one entry")
		}
		if out.OutFile == "" && out.OutDir == "" {
			// allow in-memory / default name via appResult
		}
	}
	return nil
}
