package bundler

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
	// ChunkFileNames is a filename pattern for split chunks.
	ChunkFileNames string
	// AssetFileNames is a filename pattern for emitted assets and CSS.
	AssetFileNames string
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
			if out.ChunkFileNames == "" {
				out.ChunkFileNames = o.ChunkFileNames
			}
			if out.AssetFileNames == "" {
				out.AssetFileNames = o.AssetFileNames
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
		ChunkFileNames: o.ChunkFileNames,
		AssetFileNames: o.AssetFileNames,
	}}, nil
}

func withOutput(opts Options, out OutputOptions) Options {
	opts.Format = out.Format
	opts.OutFile = out.OutFile
	opts.OutDir = out.OutDir
	opts.GlobalName = out.Name
	opts.NameTemplate = out.NameTemplate
	opts.EntryFileNames = out.EntryFileNames
	opts.ChunkFileNames = out.ChunkFileNames
	opts.AssetFileNames = out.AssetFileNames
	opts.Outputs = nil
	return opts
}
