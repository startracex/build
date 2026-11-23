package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
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

// Key hashes path, source, one fingerprint of every option that changes the
// emitted text, and an optional plugin/options suffix. The caller decides what
// the option fingerprint covers.
func (c *TranspileCache) Key(path, src, optionKey, transformKey string) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(src))
	h.Write([]byte{0})
	h.Write([]byte(optionKey))
	if transformKey != "" {
		h.Write([]byte{0})
		h.Write([]byte(transformKey))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TransformCacheKey hashes the plugin names and the define/drop settings that
// change the transform step. The order of the inputs never matters, so the same
// configuration always yields the same key.
func TransformCacheKey(pluginNames []string, define map[string]string, drop []string) string {
	h := sha256.New()
	if err := TransformCacheKeyWrite(h, pluginNames, define, drop); err != nil {
		// The input is string slices and a string map, so this cannot fail;
		// hashing a printable form keeps the key distinct if it ever does.
		h = sha256.New()
		_, _ = fmt.Fprintf(h, "%#v|%#v|%#v", pluginNames, define, drop)
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// TransformCacheKeyWrite streams the same fingerprint into w without building
// the intermediate JSON in memory. Deterministic ordering is requested
// explicitly: encoding/json/v2 does not sort map keys by default, and an
// unsorted "define" map would make the key differ from run to run.
func TransformCacheKeyWrite(w io.Writer, pluginNames []string, define map[string]string, drop []string) error {
	names := append([]string(nil), pluginNames...)
	sort.Strings(names)
	drops := make([]string, 0, len(drop))
	for _, d := range drop {
		drops = append(drops, strings.ToLower(strings.TrimSpace(d)))
	}
	sort.Strings(drops)
	return json.MarshalWrite(w, struct {
		Plugins []string          `json:"plugins"`
		Define  map[string]string `json:"define"`
		Drop    []string          `json:"drop"`
	}{names, define, drops}, json.Deterministic(true))
}

// Get loads a cache entry when present. The file is decoded as it is read, so a
// large transpile result is not first copied into a byte slice.
func (c *TranspileCache) Get(key string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := os.Open(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return Entry{}, false
	}
	defer f.Close()
	var ent Entry
	if json.UnmarshalRead(f, &ent) != nil || ent.JS == "" {
		return Entry{}, false
	}
	return ent, true
}

// Put stores a cache entry. The entry is encoded straight to the file, so a
// large transpile result is not first copied into a byte slice.
func (c *TranspileCache) Put(key string, ent Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = os.MkdirAll(c.dir, 0o755)
	path := filepath.Join(c.dir, key+".json")
	// A partial entry does not decode and is treated as a miss, but removing it
	// keeps the cache directory clean.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	err = json.MarshalWrite(f, ent)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
}
