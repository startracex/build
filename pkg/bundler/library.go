package bundler

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/sourcemap"
)

func (b *bundler) emitLibrary(entry string) (*Result, error) {
	if err := b.attachDeclarations(); err != nil {
		return nil, err
	}
	root := modpath.DisplayRoot(b.cwd, unitPaths(b, entry))
	if b.rootDir != "" {
		root = b.rootDir
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
		jsRel := libraryOutputRel(root, abs, b.opts.DepsDir)
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
		wrapLines := 0
		if b.format == FormatESM && isCommonJSSource(js) {
			js = wrapCommonJSForESM(js)
			wrapLines = cjsWrapLines
		}
		jsPath := b.outputPaths[id]
		mapName := filepath.Base(jsPath) + ".map"
		hasMap := b.sourceMap && (strings.TrimSpace(unit.jsMap) != "" || unit.verbatim)
		if hasMap {
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
		}
		files = append(files, Output{Path: jsPath, Data: js})
		b.sources[filepath.ToSlash(abs)] = jsPath
		if modpath.ModuleID(abs) == modpath.ModuleID(entry) {
			entryJS = js
		}
		if hasMap {
			mapData := sourcemap.IdentitySourceMap(filepath.Base(jsPath), modpath.DisplayRel(root, abs), unit.source)
			if strings.TrimSpace(unit.jsMap) != "" {
				mapData = sourcemap.RetargetSourceMap(unit.jsMap, filepath.Base(jsPath), modpath.DisplayRel(root, abs), unit.source)
			}
			if wrapLines > 0 {
				mapData = sourcemap.OffsetLines(mapData, wrapLines)
			}
			files = append(files, Output{Path: jsPath + ".map", Data: mapData})
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

// cjsWrapLines is how many lines wrapCommonJSForESM adds before the module
// body, so a source map for that body keeps its lines aligned.
const cjsWrapLines = 2

func wrapCommonJSForESM(code string) string {
	return "const __cjs_module = { exports: {} };\n" +
		"(function (module, exports) {\n" + code + "\n})(__cjs_module, __cjs_module.exports);\n" +
		"const __cjs_default = __cjs_module.exports;\n" +
		"export default __cjs_default;\n"
}

// libraryOutputRel returns a relative output path that never escapes the
// output directory. A module inside rootDir keeps its relative path; a bundled
// dependency is written under depsDir (node_modules when empty) using its path
// below the innermost node_modules, so a pnpm store path does not leak. Any
// other module falls back to its base name.
func libraryOutputRel(root, abs, depsDir string) string {
	jsAbs := modpath.JavascriptOutputAbs(abs)
	// A dependency goes through the deps directory even when the project root
	// contains node_modules itself: the relative path would otherwise keep the
	// pnpm store layout (node_modules/.pnpm/<pkg>@<version>/node_modules/...).
	if pkgRel, inDeps := modpath.NodeModulesRel(jsAbs); inDeps {
		if depsDir != "" {
			return depsDir + "/" + pkgRel
		}
		return "node_modules/" + pkgRel
	}
	rel := modpath.DisplayRel(root, jsAbs)
	if !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(filepath.ToSlash(jsAbs))
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
