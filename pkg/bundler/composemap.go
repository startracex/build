package bundler

import (
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/sourcemap"
)

func (b *bundler) composeAppSourceMap(js, file string) string {
	var paths []string
	for id := range b.units {
		paths = append(paths, filepath.FromSlash(id))
	}
	root := modpath.DisplayRoot(b.cwd, paths)
	if b.opts.RootDir != "" {
		root = b.opts.RootDir
	}
	var mods []sourcemap.ModuleMap
	for id, unit := range b.units {
		if unit.asset {
			continue
		}
		abs := filepath.FromSlash(id)
		display := modpath.DisplayRel(root, abs)
		jsMap := unit.jsMap
		if b.linkedMaps != nil {
			if remapped, ok := b.linkedMaps[id]; ok && remapped != "" {
				jsMap = remapped
			}
		}
		mods = append(mods, sourcemap.ModuleMap{
			Display:  display,
			Original: unit.source,
			JSMap:    jsMap,
		})
	}
	return sourcemap.ComposeLinked(b.fs, js, file, mods)
}
