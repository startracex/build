package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/build/pkg/wrap"
	"github.com/startracex/tsgo/pkg/ast"
)

// emitSplit builds entry bundles plus shared/dynamic chunks for an app graph.
//
// Every import() target becomes its own chunk, so the dynamic import stays
// lazy. Modules reachable from several entries go to a shared chunk.
func (b *bundler) emitSplit(entries []string) (*Result, error) {
	b.chunkPaths = map[string]string{}
	graph := b.staticDeps()
	asyncEdges := b.dynamicImportEdges()

	entryIDs := make([]string, 0, len(entries))
	entrySync := map[string]map[string]bool{}
	for _, e := range entries {
		id := modpath.ModuleID(e)
		entryIDs = append(entryIDs, id)
		entrySync[id] = reach(graph, id)
	}

	// Modules reachable from ≥2 entry roots (static) are shared across entries.
	entryOwner := map[string]int{}
	for _, set := range entrySync {
		for id := range set {
			entryOwner[id]++
		}
	}
	sharedMods := map[string]bool{}
	for id, n := range entryOwner {
		if n > 1 {
			// Entry roots themselves stay in their entry files.
			isEntry := slices.Contains(entryIDs, id)
			if !isEntry {
				sharedMods[id] = true
			}
		}
	}

	syncAll := map[string]bool{}
	for _, set := range entrySync {
		for id := range set {
			syncAll[id] = true
		}
	}

	asyncRoots := map[string]bool{}
	for _, targets := range asyncEdges {
		for t := range targets {
			asyncRoots[t] = true
		}
	}

	asyncOnly := map[string]map[string]bool{}
	for root := range asyncRoots {
		set := reach(graph, root)
		only := map[string]bool{root: true}
		for id := range set {
			if !syncAll[id] {
				only[id] = true
			}
		}
		asyncOnly[root] = only
	}

	ownerCount := map[string]int{}
	for _, set := range asyncOnly {
		for id := range set {
			ownerCount[id]++
		}
	}
	for id, n := range ownerCount {
		if n > 1 {
			sharedMods[id] = true
		}
	}

	var files []Output
	manifest := map[string]string{}

	// Manual chunk assignments override automatic shared/async placement.
	manualGroups := map[string][]string{}
	for id := range b.units {
		if name := b.manualChunkName(id); name != "" {
			rel := "chunks/" + name + ".js"
			b.chunkPaths[id] = rel
			delete(sharedMods, id)
			manualGroups[name] = append(manualGroups[name], id)
		}
	}
	manualNames := make([]string, 0, len(manualGroups))
	for name := range manualGroups {
		manualNames = append(manualNames, name)
	}
	sort.Strings(manualNames)
	for _, name := range manualNames {
		ids := manualGroups[name]
		sort.Strings(ids)
		rel := b.chunkOutputRel(name, name, ids, "chunks/"+name+".js")
		for _, id := range ids {
			b.chunkPaths[id] = rel
		}
		js, err := b.linkChunk(ids[0], rel, ids)
		if err != nil {
			return nil, err
		}
		out := filepath.Join(b.opts.OutDir, filepath.FromSlash(rel))
		files = append(files, Output{Path: out, Data: js})
		manifest[rel] = out
	}

	if len(sharedMods) > 0 {
		sharedIDs := keysOf(sharedMods)
		sharedRel := b.chunkOutputRel("shared", "shared", sharedIDs, "chunks/shared-"+shortHash(sharedIDs)+".js")
		for id := range sharedMods {
			if _, ok := b.chunkPaths[id]; ok {
				continue
			}
			b.chunkPaths[id] = sharedRel
		}
		root := sharedIDs[0]
		js, err := b.linkChunk(root, sharedRel, sharedIDs)
		if err != nil {
			return nil, err
		}
		out := filepath.Join(b.opts.OutDir, filepath.FromSlash(sharedRel))
		files = append(files, Output{Path: out, Data: js})
		manifest[sharedRel] = out
	}

	asyncRootList := keysOf(asyncRoots)
	sort.Strings(asyncRootList)
	rootDir := b.rootDir
	if rootDir == "" {
		rootDir = modpath.DisplayRoot(b.cwd, entries)
	}
	for _, root := range asyncRootList {
		if _, ok := b.chunkPaths[root]; ok {
			continue
		}
		mods := map[string]bool{}
		for id := range asyncOnly[root] {
			if sharedMods[id] {
				continue
			}
			if _, ok := b.chunkPaths[id]; ok {
				continue
			}
			mods[id] = true
		}
		mods[root] = true
		chunkRel := b.chunkOutputRel(
			modpath.BaseStem(root),
			entryRelPath(rootDir, root),
			[]string{root},
			"chunks/"+chunkName(root)+".js",
		)
		for id := range mods {
			if !sharedMods[id] {
				if _, ok := b.chunkPaths[id]; ok {
					continue
				}
				b.chunkPaths[id] = chunkRel
			}
		}
		js, err := b.linkChunk(root, chunkRel, nil)
		if err != nil {
			return nil, err
		}
		out := filepath.Join(b.opts.OutDir, filepath.FromSlash(chunkRel))
		files = append(files, Output{Path: out, Data: js})
		manifest[root] = out
	}

	var entryJS string
	entryOut := make(map[string]string, len(entries))
	for i, e := range entries {
		rel := filepath.Base(modpath.JavascriptOutputAbs(e))
		if r, err := filepath.Rel(rootDir, modpath.JavascriptOutputAbs(e)); err == nil {
			rel = filepath.ToSlash(r)
		}
		emitFmt := b.format
		linkFmt := emitFmt
		if linkFmt == FormatIIFE || linkFmt == FormatUMD {
			b.format = FormatCJS
		}
		// Chunk imports are relative to the entry's output directory, which a
		// name pattern can move below the output root. The directory is known
		// before linking because a hash only changes the file name.
		entryRel, fullPath := b.renderedEntryRel(rel, emitFmt, "")
		entryDir := filepath.Dir(entryRel)
		if fullPath {
			if r, err := filepath.Rel(b.opts.OutDir, entryRel); err == nil {
				entryDir = filepath.Dir(r)
			}
		}
		prevLinking := b.linkingChunk
		if entryDir == "" {
			b.linkingChunk = "index.js"
		} else {
			b.linkingChunk = entryDir + "/index.js"
		}
		linked, err := b.linkModules(modpath.ModuleID(e), nil)
		b.linkingChunk = prevLinking
		b.format = emitFmt
		if err != nil {
			return nil, err
		}
		js := wrap.WrapFormat(linked, string(emitFmt), b.opts.GlobalName)
		out := b.entryOutputPath(rel, emitFmt, js)
		if b.sourceMap {
			mapName := filepath.Base(out) + ".map"
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
			mapData := b.composeAppSourceMap(linked, filepath.Base(out))
			if emitFmt == FormatIIFE || emitFmt == FormatUMD {
				if n := sourcemap.LinesBeforeSourceComment(js); n > 0 {
					mapData = sourcemap.OffsetLines(mapData, n)
				}
				if indent := wrap.BodyIndentCols(string(emitFmt)); indent > 0 {
					mapData = sourcemap.OffsetColumns(mapData, indent)
				}
			}
			files = append(files, Output{Path: out + ".map", Data: mapData})
		}
		files = append(files, Output{Path: out, Data: js})
		entryOut[e] = out
		if i == 0 {
			entryJS = js
		}
		// An HTML page rewrites its script src from this table.
		b.sources[filepath.ToSlash(e)] = out
		manifest[e] = out
	}

	result := &Result{Files: files, Manifest: manifest, entry: entryJS}
	for _, e := range entries {
		b.emitEntryCSS(result, entryOut[e], e)
	}
	return result, nil
}

// entryOutputPath returns the full output path for one entry. A name template
// (from --outfile) is a full path; EntryFileNames is joined with OutDir; without
// either the built-in rootDir-relative layout applies.
func (b *bundler) entryOutputPath(defaultRel string, format Format, js string) string {
	pattern := b.entryPattern()
	if pattern == "" {
		return filepath.Join(b.opts.OutDir, filepath.FromSlash(defaultRel))
	}
	hash := ""
	if output.HasHashPlaceholder(pattern) {
		hash = output.ContentHash(js)
	}
	rel, fullPath := b.renderedEntryRel(defaultRel, format, hash)
	if fullPath {
		return rel
	}
	return filepath.Join(b.opts.OutDir, filepath.FromSlash(rel))
}

// entryPattern returns the entry filename pattern, preferring the full-path
// template produced by --outfile over the OutDir-joined EntryFileNames.
func (b *bundler) entryPattern() string {
	if b.opts.NameTemplate != "" {
		return b.opts.NameTemplate
	}
	return b.opts.EntryFileNames
}

// renderedEntryRel renders the entry path without joining OutDir. fullPath
// reports that the pattern is a complete path (NameTemplate), so the caller
// must not prepend OutDir.
func (b *bundler) renderedEntryRel(defaultRel string, format Format, hash string) (string, bool) {
	pattern := b.entryPattern()
	if pattern == "" {
		return defaultRel, false
	}
	rendered := output.RenderFileName(pattern, output.FileNameParts{
		Name:   modpath.BaseStem(defaultRel),
		Path:   modpath.Stem(filepath.ToSlash(defaultRel)),
		Ext:    modpath.Ext(defaultRel),
		Type:   "entry",
		Format: string(format),
		Hash:   hash,
	})
	return filepath.ToSlash(rendered), b.opts.NameTemplate != ""
}

// chunkOutputRel returns the OutDir-relative path of a chunk. Empty
// ChunkFileNames keeps the built-in fallback layout used by each chunk kind.
func (b *bundler) chunkOutputRel(name, path string, mods []string, fallback string) string {
	if b.opts.ChunkFileNames == "" {
		return fallback
	}
	hash := ""
	if output.HasHashPlaceholder(b.opts.ChunkFileNames) {
		hash = shortHash(mods)
	}
	return filepath.ToSlash(output.RenderFileName(b.opts.ChunkFileNames, output.FileNameParts{
		Name:   name,
		Path:   path,
		Ext:    ".js",
		Type:   "chunk",
		Format: string(FormatESM),
		Hash:   hash,
	}))
}

// entryRelPath returns the rootDir-relative path of an entry without its
// extension, used as the [path] value in a filename pattern.
func entryRelPath(rootDir, abs string) string {
	p := modpath.JavascriptOutputAbs(abs)
	if r, err := filepath.Rel(rootDir, p); err == nil {
		return modpath.Stem(filepath.ToSlash(r))
	}
	return modpath.BaseStem(p)
}

func (b *bundler) linkChunk(entryID, chunkRel string, preserve []string) (string, error) {
	prev := b.linkingChunk
	b.linkingChunk = chunkRel
	js, err := b.linkModules(entryID, preserve)
	b.linkingChunk = prev
	if err != nil {
		return "", err
	}
	return wrap.WrapFormat(js, string(FormatESM), ""), nil
}

func (b *bundler) staticDeps() map[string][]string {
	out := map[string][]string{}
	for id, unit := range b.units {
		file := unit.file
		if file == nil {
			file = jsedit.ParseEmittedJS(unit.code)
		}
		for _, spec := range specifiers(file) {
			resolved, external, err := b.resolve(filepath.FromSlash(id), spec)
			if err != nil || external || resolved == "" {
				continue
			}
			dep := modpath.ModuleID(resolved)
			if _, loaded := b.units[dep]; !loaded {
				// The graph must not point at a module that was never loaded:
				// a chunk built from such an id has no entry module.
				continue
			}
			out[id] = append(out[id], dep)
		}
	}
	return out
}

// dynamicImportEdges lists the import() targets of each module. A require call
// is synchronous and stays in its bundle, so it must not become a chunk root.
func (b *bundler) dynamicImportEdges() map[string]map[string]bool {
	edges := map[string]map[string]bool{}
	for id, unit := range b.units {
		file := jsedit.ParseEmittedJS(unit.code)
		if file.Statements == nil {
			continue
		}
		ast.ForEachDynamicImportOrRequireCall(file, true, false, func(node *ast.Node, argument *ast.Expression) bool {
			if argument == nil || !isImportCall(node) {
				return false
			}
			spec := argument.Text()
			if spec == "" {
				return false
			}
			resolved, external, err := b.resolve(filepath.FromSlash(id), spec)
			if err != nil || external || resolved == "" {
				return false
			}
			target := modpath.ModuleID(resolved)
			if _, loaded := b.units[target]; !loaded {
				return false
			}
			if edges[id] == nil {
				edges[id] = map[string]bool{}
			}
			edges[id][target] = true
			return false
		})
	}
	return edges
}

// isImportCall reports whether node is an import() call rather than require().
func isImportCall(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindCallExpression {
		return false
	}
	callee := node.AsCallExpression().Expression
	return callee != nil && callee.Kind == ast.KindImportKeyword
}

func reach(graph map[string][]string, start string) map[string]bool {
	seen := map[string]bool{}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		for _, d := range graph[n] {
			if !seen[d] {
				stack = append(stack, d)
			}
		}
	}
	return seen
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func shortHash(parts []string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])[:8]
}

func (b *bundler) manualChunkName(moduleID string) string {
	if b == nil || len(b.opts.ManualChunks) == 0 {
		return ""
	}
	bestKey, bestName := "", ""
	for key, name := range b.opts.ManualChunks {
		if key == "" || name == "" {
			continue
		}
		if strings.Contains(moduleID, key) && len(key) > len(bestKey) {
			bestKey, bestName = key, name
		}
	}
	if bestName == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, bestName)
}

func chunkName(moduleID string) string {
	base := modpath.BaseStem(moduleID)
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, base)
	return base + "-" + shortHash([]string{moduleID})
}
