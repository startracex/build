package bundler

import (
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/dts"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/resolve"
)

// attachDeclarations fills library units from a tsgo program emit so inferred
// types come from the checker. A file the program does not emit falls back to
// single-file declaration transpile.
func (b *bundler) attachDeclarations() error {
	if !b.decl {
		return nil
	}
	var absPaths []string
	for id := range b.units {
		abs := filepath.FromSlash(id)
		if modpath.IsTypeScriptPath(abs) {
			absPaths = append(absPaths, abs)
		}
	}
	checked, err := dts.EmitProgram(b.ctx, absPaths, dts.Options{
		Target:           b.target,
		Jsx:              b.jsx,
		DeclarationMap:   b.declMap,
		CustomConditions: resolve.ExportConditions(b.opts.Conditions),
		CompilerOptions:  b.compilerOpts,
		BaseURL:          b.opts.BaseURL,
	})
	if err != nil {
		return err
	}
	for id, unit := range b.units {
		abs := filepath.FromSlash(id)
		if got, ok := checked[dts.Key(abs)]; ok && strings.TrimSpace(got.Text) != "" {
			unit.dts = dts.StripMapComment(got.Text)
			unit.dtsMap = got.SourceMap
			continue
		}
		if !modpath.IsTypeScriptPath(abs) || strings.TrimSpace(unit.dts) != "" {
			continue
		}
		text, err := b.declarationText(abs, unit.source)
		if err != nil {
			return err
		}
		unit.dts = text
	}
	return nil
}
