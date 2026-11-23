package bundler

import (
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/modpath"
)

// emitEntryCSS writes one concatenated stylesheet beside the entry JS output.
func (b *bundler) emitEntryCSS(result *Result, jsOut, entryAbs string) {
	if b.outMode != ModeApp {
		return
	}
	css, sources := b.cssForEntry(entryAbs)
	if css == "" {
		return
	}
	cssPath := jsOut
	if cssPath == "" {
		base := filepath.Base(javascriptOutputAbs(entryAbs))
		if b.opts.OutDir != "" {
			cssPath = filepath.Join(b.opts.OutDir, base)
		} else {
			cssPath = base
		}
	}
	cssPath = modpath.ReplaceExt(cssPath, ".css")
	result.Files = append(result.Files, Output{Path: cssPath, Data: css})
	if result.Manifest == nil {
		result.Manifest = map[string]string{}
	}
	for src := range sources {
		result.Manifest[src] = cssPath
	}
}

func (b *bundler) cssForEntry(entryAbs string) (string, map[string]bool) {
	graph := b.staticDeps()
	reached := reach(graph, moduleID(entryAbs))
	ids := keysOf(reached)
	var parts []string
	sources := map[string]bool{}
	for _, id := range ids {
		au := b.assets[id]
		if au == nil || au.Kind != asset.CSS {
			continue
		}
		parts = append(parts, string(au.Data))
		sources[au.Source] = true
	}
	return strings.Join(parts, "\n"), sources
}
