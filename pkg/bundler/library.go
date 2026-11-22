package bundler

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/sourcemap"
)

func (b *bundler) emitLibrary(entry string) (*Result, error) {
	if err := b.attachDeclarations(); err != nil {
		return nil, err
	}
	root := modpath.DisplayRoot(unitPaths(b, entry))
	if b.opts.RootDir != "" {
		root = b.opts.RootDir
	}
	var files []Output
	var entryJS string
	ids := make([]string, 0, len(b.units))
	for id := range b.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		unit := b.units[id]
		abs := filepath.FromSlash(id)
		jsRel := modpath.DisplayRel(root, modpath.JavascriptOutputAbs(abs))
		js, err := rewriteRelativeSpecifiers(unit.code, abs, b, false)
		if err != nil {
			return nil, err
		}
		mapName := filepath.Base(jsRel) + ".map"
		if b.sourceMap && strings.TrimSpace(unit.jsMap) != "" {
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
		}
		jsPath := jsRel
		if b.opts.OutDir != "" {
			jsPath = filepath.Join(b.opts.OutDir, filepath.FromSlash(jsRel))
		}
		files = append(files, Output{Path: jsPath, Data: js})
		if moduleID(abs) == moduleID(entry) {
			entryJS = js
		}
		if b.sourceMap && strings.TrimSpace(unit.jsMap) != "" {
			mapPath := jsPath + ".map"
			files = append(files, Output{Path: mapPath, Data: sourcemap.RetargetSourceMap(unit.jsMap, filepath.Base(jsRel), modpath.DisplayRel(root, abs), unit.source)})
		}
		if b.decl && strings.TrimSpace(unit.dts) != "" {
			dts, err := rewriteRelativeSpecifiers(unit.dts, abs, b, true)
			if err != nil {
				return nil, err
			}
			dtsRel := strings.TrimSuffix(jsRel, filepath.Ext(jsRel)) + ".d.ts"
			dtsPath := dtsRel
			if b.opts.OutDir != "" {
				dtsPath = filepath.Join(b.opts.OutDir, filepath.FromSlash(dtsRel))
			}
			mapName := filepath.Base(dtsRel) + ".map"
			if b.declMap && strings.TrimSpace(unit.dtsMap) != "" {
				dts = strings.TrimRight(jsedit.StripSourceMappingComment(dts), "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
			}
			files = append(files, Output{Path: dtsPath, Data: dts})
			if b.declMap && strings.TrimSpace(unit.dtsMap) != "" {
				files = append(files, Output{
					Path: dtsPath + ".map",
					Data: sourcemap.RelativizeDeclarationMap(unit.dtsMap, filepath.Base(dtsRel), root),
				})
			}
		}
	}
	if entryJS == "" {
		for _, file := range files {
			if isJavaScriptOutput(file.Path) {
				entryJS = file.Data
				break
			}
		}
	}
	return &Result{Files: files, entry: entryJS}, nil
}
