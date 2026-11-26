package link

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/startracex/build/pkg/filepath"
)

type linkContext struct {
	opts Options
}

// chunkSpec returns the specifier from the current output file to a target chunk; both paths are relative to the output root, so a subdirectory entry uses "..", and an empty LinkingChunk means the current file is at the root.
func (c *linkContext) chunkSpec(target string) string {
	rel := target
	if base := c.opts.LinkingChunk; base != "" {
		if dir := filepath.Dir(base); dir != "" {
			if r, err := filepath.Rel(dir, target); err == nil {
				rel = r
			}
		}
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func (c *linkContext) resolve(importer, spec string) (resolved string, external bool, err error) {
	if c.opts.Resolver == nil {
		return "", false, fmt.Errorf("link: no resolver")
	}
	return c.opts.Resolver.Resolve(importer, spec)
}

func (c *linkContext) chunkRel(moduleID string) string {
	if c.opts.Chunks == nil {
		return ""
	}
	rel, ok := c.opts.Chunks.ChunkPath(moduleID)
	if !ok {
		return ""
	}
	return rel
}

func (c *linkContext) splitDynamicImport(moduleID string) (string, bool) {
	if c.opts.Chunks == nil {
		return "", false
	}
	rel, ok := c.opts.Chunks.ChunkPath(moduleID)
	if !ok || rel == "" || rel == c.opts.LinkingChunk {
		return "", false
	}
	return fmt.Sprintf("import(%s)", strconv.Quote(c.chunkSpec(rel))), true
}
