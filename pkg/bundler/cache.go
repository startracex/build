package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type transpileCache struct {
	dir string
	mu  sync.Mutex
}

type cacheEntry struct {
	JS  string `json:"js"`
	Map string `json:"map,omitempty"`
}

func newTranspileCache(dir string) *transpileCache {
	if dir == "" {
		return nil
	}
	return &transpileCache{dir: dir}
}

func (c *transpileCache) key(path, src string, moduleKind, target, jsx int, sourceMap bool) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(src))
	h.Write([]byte{0})
	var meta [16]byte
	meta[0] = byte(moduleKind)
	meta[1] = byte(target)
	meta[2] = byte(jsx)
	if sourceMap {
		meta[3] = 1
	}
	h.Write(meta[:])
	return hex.EncodeToString(h.Sum(nil))
}

func (c *transpileCache) get(key string) (cacheEntry, bool) {
	if c == nil {
		return cacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return cacheEntry{}, false
	}
	var ent cacheEntry
	if json.Unmarshal(raw, &ent) != nil || ent.JS == "" {
		return cacheEntry{}, false
	}
	return ent, true
}

func (c *transpileCache) put(key string, ent cacheEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = os.MkdirAll(c.dir, 0o755)
	raw, err := json.Marshal(ent)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(c.dir, key+".json"), raw, 0o644)
}
