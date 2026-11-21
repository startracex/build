package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
)

// emitSplit builds entry bundles plus shared/dynamic chunks for a multi-entry
// or --splitting app graph.
func (b *bundler) emitSplit(entries []string) (*Result, error) {
	b.chunkPaths = map[string]string{}
	graph := b.staticDeps()
	asyncEdges := b.dynamicImportEdges()

	entryIDs := make([]string, 0, len(entries))
	entrySync := map[string]map[string]bool{}
	for _, e := range entries {
		id := moduleID(e)
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
			isEntry := false
			for _, eid := range entryIDs {
				if eid == id {
					isEntry = true
					break
				}
			}
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

	if len(sharedMods) > 0 {
		sharedRel := "chunks/shared-" + shortHash(keysOf(sharedMods)) + ".js"
		for id := range sharedMods {
			b.chunkPaths[id] = sharedRel
		}
		root := keysOf(sharedMods)[0]
		js, err := b.linkChunk(root, sharedRel, keysOf(sharedMods))
		if err != nil {
			return nil, err
		}
		out := filepath.Join(b.opts.OutDir, filepath.FromSlash(sharedRel))
		files = append(files, Output{Path: out, Data: js})
		manifest[sharedRel] = out
	}

	asyncRootList := keysOf(asyncRoots)
	sort.Strings(asyncRootList)
	for _, root := range asyncRootList {
		mods := map[string]bool{}
		for id := range asyncOnly[root] {
			if sharedMods[id] {
				continue
			}
			mods[id] = true
		}
		mods[root] = true
		chunkRel := "chunks/" + chunkName(root) + ".js"
		for id := range mods {
			if !sharedMods[id] {
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
	rootDir := b.opts.RootDir
	if rootDir == "" {
		rootDir = displayRoot(entries)
	}
	for i, e := range entries {
		rel := filepath.Base(javascriptOutputAbs(e))
		if r, err := filepath.Rel(rootDir, javascriptOutputAbs(e)); err == nil {
			rel = filepath.ToSlash(r)
		}
		js, err := b.linkModules(moduleID(e), nil)
		if err != nil {
			return nil, err
		}
		js = wrapFormat(js, b.format, b.opts.GlobalName)
		out := filepath.Join(b.opts.OutDir, filepath.FromSlash(rel))
		if b.sourceMap {
			mapName := filepath.Base(out) + ".map"
			js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + mapName + "\n"
			files = append(files, Output{Path: out + ".map", Data: b.composeAppSourceMap(js, filepath.Base(out))})
		}
		files = append(files, Output{Path: out, Data: js})
		if i == 0 {
			entryJS = js
		}
		manifest[e] = out
	}

	return &Result{Files: files, Manifest: manifest, entry: entryJS}, nil
}

func (b *bundler) linkChunk(entryID, chunkRel string, preserve []string) (string, error) {
	prev := b.linkingChunk
	b.linkingChunk = chunkRel
	js, err := b.linkModules(entryID, preserve)
	b.linkingChunk = prev
	if err != nil {
		return "", err
	}
	return wrapFormat(js, FormatESM, ""), nil
}

func (b *bundler) staticDeps() map[string][]string {
	out := map[string][]string{}
	for id, unit := range b.units {
		file := unit.file
		if file == nil {
			file = parseEmittedJS(unit.code)
		}
		for _, spec := range specifiers(file) {
			resolved, external, err := b.resolve(filepath.FromSlash(id), spec)
			if err != nil || external || resolved == "" {
				continue
			}
			out[id] = append(out[id], moduleID(resolved))
		}
	}
	return out
}

func (b *bundler) dynamicImportEdges() map[string]map[string]bool {
	edges := map[string]map[string]bool{}
	for id, unit := range b.units {
		file := parseEmittedJS(unit.code)
		if file.Statements == nil {
			continue
		}
		ast.ForEachDynamicImportOrRequireCall(file, true, false, func(_ *ast.Node, argument *ast.Expression) bool {
			if argument == nil {
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
			if edges[id] == nil {
				edges[id] = map[string]bool{}
			}
			edges[id][moduleID(resolved)] = true
			return false
		})
	}
	return edges
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

func chunkName(moduleID string) string {
	base := strings.TrimSuffix(path.Base(moduleID), path.Ext(moduleID))
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, base)
	return base + "-" + shortHash([]string{moduleID})
}
