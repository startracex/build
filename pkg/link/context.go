package link

import (
	"fmt"
	"strconv"
)

type linkContext struct {
	opts Options
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
	return fmt.Sprintf("import(%s)", strconv.Quote("./"+rel)), true
}
