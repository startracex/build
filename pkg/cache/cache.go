package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// TranspileCache stores transpile results keyed by content hash on disk.
type TranspileCache struct {
	dir string
	mu  sync.Mutex
}

// Entry is one cached transpile result.
type Entry struct {
	JS  string `json:"js"`
	Map string `json:"map,omitempty"`
}

// NewTranspileCache returns a disk cache, or nil when dir is empty.
func NewTranspileCache(dir string) *TranspileCache {
	if dir == "" {
		return nil
	}
	return &TranspileCache{dir: dir}
}

// Key hashes path, source, emit options, and an optional plugin/options suffix.
func (c *TranspileCache) Key(path, src string, moduleKind, target, jsx int, sourceMap bool, transformKey string) string {
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
	if transformKey != "" {
		h.Write([]byte{0})
		h.Write([]byte(transformKey))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TransformCacheKey hashes plugin names plus define/drop that affect transforms.
func TransformCacheKey(pluginNames []string, define map[string]string, drop []string) string {
	h := sha256.New()
	sort.Strings(pluginNames)
	for _, name := range pluginNames {
		h.Write([]byte(name))
		h.Write([]byte{0})
	}
	keys := make([]string, 0, len(define))
	for k := range define {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte("d"))
		h.Write([]byte(k))
		h.Write([]byte(define[k]))
	}
	sort.Strings(drop)
	for _, d := range drop {
		h.Write([]byte("p"))
		h.Write([]byte(strings.ToLower(strings.TrimSpace(d))))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// Get loads a cache entry when present.
func (c *TranspileCache) Get(key string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return Entry{}, false
	}
	var ent Entry
	if json.Unmarshal(raw, &ent) != nil || ent.JS == "" {
		return Entry{}, false
	}
	return ent, true
}

// Put stores a cache entry.
func (c *TranspileCache) Put(key string, ent Entry) {
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