package bundler

import "github.com/startracex/build/pkg/link"

type linkResolver struct{ b *bundler }

func (r linkResolver) Resolve(importer, spec string) (string, bool, error) {
	return r.b.resolve(importer, spec)
}

type linkChunks struct{ b *bundler }

func (c linkChunks) ChunkPath(moduleID string) (rel string, ok bool) {
	if c.b == nil || c.b.chunkPaths == nil {
		return "", false
	}
	rel, ok = c.b.chunkPaths[moduleID]
	return rel, ok && rel != ""
}

func (b *bundler) linkModules(entryID string, preserve []string) (string, error) {
	mods := make(map[string]link.Module, len(b.units))
	for id, unit := range b.units {
		mods[id] = link.Module{ID: id, Code: unit.code, JSMap: unit.jsMap}
	}
	res, err := link.Link(mods, entryID, link.Options{
		Format:          string(b.format),
		LinkingChunk:    b.linkingChunk,
		Chunks:          linkChunks{b},
		Resolver:        linkResolver{b},
		PreserveExports: preserve,
	})
	if err != nil {
		return "", err
	}
	b.linkedMaps = res.Maps
	return res.Code, nil
}
