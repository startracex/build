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
	outs := append([]OutputOptions(nil), o.Outputs...)
	if len(outs) == 0 {
		outs = []OutputOptions{{}}
	}
	for i := range outs {
		format, err := normalizeFormat(outs[i].Format)
		if err != nil {
			return nil, err
		}
		outs[i].Format = format
	}
	return outs, nil
}
