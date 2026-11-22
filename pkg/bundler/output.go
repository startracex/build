package bundler

import "github.com/startracex/build/pkg/output"

// DeriveOutFile picks an extension for dual-format outfile emission.
func DeriveOutFile(base string, format Format) string {
	return output.DeriveOutFile(base, string(format))
}

// DeriveOutDir nests format under a shared outdir for multi-format lib builds.
func DeriveOutDir(base string, format Format) string {
	return output.DeriveOutDir(base, string(format))
}

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
