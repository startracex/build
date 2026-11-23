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
	b.outputPaths = nil
	if b.opts.NameTemplate != "" && b.opts.OutFile != "" {
		hasHash := HasHashPlaceholder(b.opts.NameTemplate)
		b.outputPaths = make(map[string]string, len(b.units))
		for id, unit := range b.units {
			abs := filepath.FromSlash(id)
			jsRel := modpath.DisplayRel(root, modpath.JavascriptOutputAbs(abs))
			name := strings.TrimSuffix(filepath.Base(jsRel), filepath.Ext(jsRel))
			hash := ""
			if hasHash {
				hash = ContentHash(unit.code)
			}
			b.outputPaths[id] = RenderEntryFileName(b.opts.NameTemplate, name, b.format, hash)
		}
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
		templated := len(b.outputPaths) > 0
		jsPath := jsRel
		if templated {
			jsPath = b.outputPaths[id]
		} else if b.opts.OutDir != "" {
			jsPath = filepath.Join(b.opts.OutDir, filepath.FromSlash(jsRel))
		}
		mapName := filepath.Base(jsPath) + ".map"
		if b.sourceMap && strings.TrimSpace(unit.jsMap) != "" {
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
		}
		files = append(files, Output{Path: jsPath, Data: js})
		if moduleID(abs) == moduleID(entry) {
			entryJS = js
		}
		if b.sourceMap && strings.TrimSpace(unit.jsMap) != "" {
			mapPath := jsPath + ".map"
			files = append(files, Output{Path: mapPath, Data: sourcemap.RetargetSourceMap(unit.jsMap, filepath.Base(jsPath), modpath.DisplayRel(root, abs), unit.source)})
		}
		if b.decl && strings.TrimSpace(unit.dts) != "" {
			dts, err := rewriteRelativeSpecifiers(unit.dts, abs, b, true)
			if err != nil {
				return nil, err
			}
			dtsPath := strings.TrimSuffix(jsPath, filepath.Ext(jsPath)) + ".d.ts"
			if !templated && b.opts.OutDir == "" {
				dtsPath = strings.TrimSuffix(jsRel, filepath.Ext(jsRel)) + ".d.ts"
			}
			mapName := filepath.Base(dtsPath) + ".map"
			if b.declMap && strings.TrimSpace(unit.dtsMap) != "" {
				dts = strings.TrimRight(jsedit.StripSourceMappingComment(dts), "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
			}
			files = append(files, Output{Path: dtsPath, Data: dts})
			if b.declMap && strings.TrimSpace(unit.dtsMap) != "" {
				files = append(files, Output{
					Path: dtsPath + ".map",
					Data: sourcemap.RelativizeDeclarationMap(unit.dtsMap, filepath.Base(dtsPath), root),
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
