package bundler

import (
	"fmt"
	"path/filepath"
	"regexp"
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
	ids := make([]string, 0, len(b.units))
	for id := range b.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pattern := b.opts.NameTemplate
	fullPathPattern := pattern != ""
	if !fullPathPattern {
		pattern = b.opts.EntryFileNames
	}
	templated := pattern != ""
	hasHash := templated && HasHashPlaceholder(pattern)
	b.outputPaths = make(map[string]string, len(b.units))
	for _, id := range ids {
		unit := b.units[id]
		abs := filepath.FromSlash(id)
		jsRel := libraryOutputRel(root, abs)
		outPath := jsRel
		if b.opts.OutDir != "" {
			outPath = filepath.Join(b.opts.OutDir, filepath.FromSlash(jsRel))
		}
		if templated {
			name := modpath.BaseStem(jsRel)
			pathValue := modpath.Stem(filepath.ToSlash(jsRel))
			hash := ""
			if hasHash {
				hash = ContentHash(unit.code)
			}
			rendered := RenderEntryFileName(pattern, name, pathValue, b.format, hash)
			outPath = rendered
			if !fullPathPattern {
				outPath = filepath.Join(b.opts.OutDir, filepath.FromSlash(rendered))
			}
		}
		b.outputPaths[id] = b.uniqueOutputPath(outPath)
	}
	var files []Output
	var entryJS string
	for _, id := range ids {
		unit := b.units[id]
		abs := filepath.FromSlash(id)
		js, err := rewriteRelativeSpecifiers(unit.code, abs, b, false)
		if err != nil {
			return nil, err
		}
		if b.format == FormatESM && isCommonJSSource(js) {
			js = wrapCommonJSForESM(js)
		}
		jsPath := b.outputPaths[id]
		mapName := filepath.Base(jsPath) + ".map"
		if b.sourceMap && strings.TrimSpace(unit.jsMap) != "" {
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
		}
		files = append(files, Output{Path: jsPath, Data: js})
		b.sources[filepath.ToSlash(abs)] = jsPath
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
			dtsPath := modpath.ReplaceExt(jsPath, ".d.ts")
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

var esmStatement = regexp.MustCompile(`(?m)^\s*(?:import\s|export\s|export\s*\{)`)

func isCommonJSSource(code string) bool {
	if esmStatement.MatchString(code) {
		return false
	}
	return strings.Contains(code, "module.exports") || strings.Contains(code, "exports.")
}

func wrapCommonJSForESM(code string) string {
	return "const __cjs_module = { exports: {} };\n" +
		"(function (module, exports) {\n" + code + "\n})(__cjs_module, __cjs_module.exports);\n" +
		"const __cjs_default = __cjs_module.exports;\n" +
		"export default __cjs_default;\n"
}

// libraryOutputRel returns a relative output path that never escapes the
// output directory. Modules outside rootDir (notably pnpm node_modules) keep
// their node_modules/... suffix; others fall back to their base name.
func libraryOutputRel(root, abs string) string {
	jsAbs := modpath.JavascriptOutputAbs(abs)
	rel := modpath.DisplayRel(root, jsAbs)
	if !strings.HasPrefix(rel, "..") {
		return rel
	}
	slash := filepath.ToSlash(jsAbs)
	if idx := strings.Index(slash, "/node_modules/"); idx >= 0 {
		return slash[idx+1:]
	}
	return filepath.Base(slash)
}

func (b *bundler) uniqueOutputPath(path string) string {
	if b.usedOutputPaths == nil {
		b.usedOutputPaths = map[string]bool{}
	}
	if !b.usedOutputPaths[path] {
		b.usedOutputPaths[path] = true
		return path
	}
	ext := modpath.Ext(path)
	stem := modpath.Stem(path)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !b.usedOutputPaths[candidate] {
			b.usedOutputPaths[candidate] = true
			return candidate
		}
	}
}
