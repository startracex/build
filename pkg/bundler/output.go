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
	// NameTemplate marks OutFile as a full per-entry filename pattern.
	NameTemplate string
	// EntryFileNames is a filename pattern joined with OutDir.
	EntryFileNames string
}

// RenderEntryFileName expands [name], [path], [format], [hash], and [hash:N].
func RenderEntryFileName(pattern, name, path string, format Format, hash string) string {
	return output.RenderEntryFileName(pattern, name, path, string(format), hash)
}

// ContentHash returns the digest used by [hash] placeholders.
func ContentHash(data string) string {
	return output.ContentHash(data)
}

// HasHashPlaceholder reports whether pattern contains [hash] or [hash:N].
func HasHashPlaceholder(pattern string) bool {
	return output.HasHashPlaceholder(pattern)
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
			if out.NameTemplate == "" {
				out.NameTemplate = o.NameTemplate
			}
			if out.EntryFileNames == "" {
				out.EntryFileNames = o.EntryFileNames
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
		Format:         format,
		OutFile:        o.OutFile,
		OutDir:         o.OutDir,
		Name:           o.GlobalName,
		NameTemplate:   o.NameTemplate,
		EntryFileNames: o.EntryFileNames,
	}}, nil
}

func withOutput(opts Options, out OutputOptions) Options {
	opts.Format = out.Format
	opts.OutFile = out.OutFile
	opts.OutDir = out.OutDir
	opts.GlobalName = out.Name
	opts.NameTemplate = out.NameTemplate
	opts.EntryFileNames = out.EntryFileNames
	opts.Outputs = nil
	return opts
}
