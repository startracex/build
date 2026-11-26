package bundler

import (
	"fmt"
	"strings"

	"github.com/startracex/build/pkg/dts"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/build/pkg/sourcemap"
)

// attachDeclarations fills library units from the checker's program emit, falling back to single-file transpile.
func (b *bundler) attachDeclarations() error {
	if !b.output.declaration() {
		return nil
	}
	var absPaths []string
	for id := range b.units {
		abs := filepath.FromSlash(id)
		if modpath.IsTypeScript(abs) {
			absPaths = append(absPaths, abs)
		}
	}
	checked, err := dts.EmitProgram(b.ctx, absPaths, dts.Options{
		Target:           b.target,
		Jsx:              b.jsx,
		DeclarationMap:   b.output.declarationMap(),
		CustomConditions: resolve.ExportConditions(b.opts.Conditions),
		CompilerOptions:  b.compilerOpts,
		BaseURL:          b.opts.BaseURL,
		FS:               b.fs,
		CWD:              b.cwd,
		RootDir:          b.rootDir,
	})
	if err != nil {
		return err
	}
	for id, unit := range b.units {
		abs := filepath.FromSlash(id)
		if got, ok := checked[dts.Key(abs)]; ok && strings.TrimSpace(got.Text) != "" {
			unit.dts = jsedit.StripSourceMappingComment(got.Text)
			unit.dtsMap, err = sourcemap.ParseMap(got.SourceMap)
			if err != nil {
				return fmt.Errorf("parse declaration map for %s: %w", abs, err)
			}
			continue
		}
		if !modpath.IsTypeScript(abs) || strings.TrimSpace(unit.dts) != "" {
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
