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
	// SourceMap writes .map files. Nil means no, matching tsc.
	SourceMap *bool
	// Declaration writes .d.ts files in library mode only; nil disables it, matching tsc.
	Declaration *bool
	// DeclarationMap writes .d.ts.map files when Declaration is on; nil disables it.
	DeclarationMap *bool
	// Manifest writes a JSON map of source assets to emitted paths.
	Manifest bool
	// PublicPath prefixes emitted asset URLs for ?url and file imports.
	PublicPath string
	// ManualChunks maps a module path substring to a chunk name under chunks/; app mode only.
	ManualChunks map[string]string
	// DepsDir moves bundled packages under this directory in library mode; empty keeps node_modules.
	DepsDir string
}

func (o OutputOptions) declaration() bool {
	return o.Declaration != nil && *o.Declaration
}

func (o OutputOptions) declarationMap() bool {
	return o.declaration() && o.DeclarationMap != nil && *o.DeclarationMap
}

func (o OutputOptions) sourceMap() bool {
	return o.SourceMap != nil && *o.SourceMap
}

// anySourceMap reports whether the graph must be transpiled with maps for at least one output.
func anySourceMap(outputs []OutputOptions) bool {
	for _, out := range outputs {
		if out.sourceMap() {
			return true
		}
	}
	return false
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
