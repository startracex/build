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
	// JSMap is the transpile source map for Code (optional).
	JSMap string
}

// Options configure a Link call.
type Options struct {
	Format       string // "esm", "cjs", "iife", "umd"
	LinkingChunk string
	Chunks       ChunkTable
	Resolver     Resolver
	// PreserveExports lists module ids whose public exports stay live even when
	// they are not the link entry (used for shared chunks).
	PreserveExports []string
}

// Result is the linked script plus per-module maps remapped through linker edits.
type Result struct {
	Code string
	// Maps maps module id -> source map JSON for the rendered module body.
	Maps map[string]string
}

// Link scope-hoists mods into one script or module body.
func Link(mods map[string]Module, entryID string, opts Options) (Result, error) {
	ctx := &linkContext{opts: opts}
	parsed := map[string]*esModule{}
	var order []string
	for id, mod := range mods {
		em, err := parseLinkedModule(ctx, id, mod.Code, mod.Path)
		if err != nil {
			return Result{}, err
		}
		em.jsMap = mod.JSMap
		em.effectsFree = treeshake.ModuleSideEffectsFree(em.osPath())
		parsed[id] = em
		order = append(order, id)
	}
	slices.Sort(order)
	entry := parsed[entryID]
	if entry == nil {
		return Result{}, fmt.Errorf("missing entry module")
	}
	if err := markLive(parsed, entry); err != nil {
		return Result{}, err
	}
	for _, id := range opts.PreserveExports {
		m := parsed[id]
		if m == nil {
			continue
		}
		names, err := m.publicNames(parsed, map[string]bool{})
		if err != nil {
			return Result{}, err
		}
		for _, name := range names {
			if err := markExport(parsed, m, name, map[string]bool{}); err != nil {
				return Result{}, err
			}
		}
	}
	names := &namer{used: map[string]int{}, taken: map[string]struct{}{}}
	helper := ""
	if anyCJSDynamicMap(parsed) {
		helper = names.take("__commonJS")
	}
	if err := assignNames(parsed, order, names); err != nil {
		return Result{}, err
	}
	format := opts.Format
	if format == "" {
		format = "esm"
	}
	if format == "iife" || format == "umd" {
		format = "cjs"
	}
	code := renderLinked(parsed, order, entry, format, helper)
	maps := map[string]string{}
	for id, m := range parsed {
		if m.linkedMap != "" {
			maps[id] = m.linkedMap
		}
	}
	return Result{Code: code, Maps: maps}, nil
}
