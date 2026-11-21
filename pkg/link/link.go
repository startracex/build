package link

import (
	"fmt"
	"slices"

	"github.com/startracex/build/pkg/treeshake"
)

// Resolver resolves import specifiers during linking.
type Resolver interface {
	Resolve(importer, spec string) (path string, external bool, err error)
}

// ChunkTable maps module ids to emitted chunk paths for code splitting.
type ChunkTable interface {
	ChunkPath(moduleID string) (rel string, ok bool)
}

// Module is one unit in the link graph.
type Module struct {
	ID   string // slash path id
	Code string
	Path string // os path if needed
}

// Options configure a Link call.
type Options struct {
	Format       string // "esm", "cjs", "iife", "umd"
	LinkingChunk string
	Chunks       ChunkTable
	Resolver     Resolver
}

// Link scope-hoists mods into one script or module body.
func Link(mods map[string]Module, entryID string, opts Options) (string, error) {
	ctx := &linkContext{opts: opts}
	parsed := map[string]*esModule{}
	var order []string
	for id, mod := range mods {
		em, err := parseLinkedModule(ctx, id, mod.Code, mod.Path)
		if err != nil {
			return "", err
		}
		em.effectsFree = treeshake.ModuleSideEffectsFree(em.osPath())
		parsed[id] = em
		order = append(order, id)
	}
	slices.Sort(order)
	entry := parsed[entryID]
	if entry == nil {
		return "", fmt.Errorf("missing entry module")
	}
	if err := markLive(parsed, entry); err != nil {
		return "", err
	}
	names := &namer{used: map[string]int{}, taken: map[string]struct{}{}}
	helper := ""
	if anyCJSDynamicMap(parsed) {
		helper = names.take("__commonJS")
	}
	if err := assignNames(parsed, order, names); err != nil {
		return "", err
	}
	format := opts.Format
	if format == "" {
		format = "esm"
	}
	if format == "iife" || format == "umd" {
		format = "cjs"
	}
	return renderLinked(parsed, order, entry, format, helper), nil
}