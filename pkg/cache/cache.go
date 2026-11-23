package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// TranspileCache stores transpile results keyed by content hash. The entries
// live under dir, on fs when one is set and on the OS file system otherwise.
type TranspileCache struct {
	dir string
	fs  vfs.FS
	mu  sync.Mutex
}

// Entry is one cached transpile result.
type Entry struct {
	JS  string `json:"js"`
	Map string `json:"map,omitempty"`
}

// NewTranspileCache returns a cache under dir, or nil when dir is empty. A nil
// fs stores entries on the OS file system; an injected fs keeps them in the same
// tree as the build, so a virtual or wasm build caches where it reads.
func NewTranspileCache(fs vfs.FS, dir string) *TranspileCache {
	if dir == "" {
		return nil
	}
	return &TranspileCache{dir: dir, fs: fs}
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
	path := filepath.Join(c.dir, key+".json")
	if c.fs != nil {
		rooted, ok := tspath.TryRootedFilePathFromAbsolute(path)
		if !ok {
			return Entry{}, false
		}
		text, ok := c.fs.ReadFile(rooted)
		if !ok {
			return Entry{}, false
		}
		var ent Entry
		if json.Unmarshal([]byte(text), &ent) != nil || ent.JS == "" {
			return Entry{}, false
		}
		return ent, true
	}
	f, err := os.Open(filepath.Native(path))
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
	path := filepath.Join(c.dir, key+".json")
	if c.fs != nil {
		// WriteFile creates the directory of the entry when the fs supports it.
		rooted, ok := tspath.TryRootedFilePathFromAbsolute(path)
		if !ok {
			return
		}
		buf, err := json.Marshal(ent)
		if err != nil {
			return
		}
		if c.fs.WriteFile(rooted, string(buf)) != nil {
			_ = c.fs.Remove(tspath.RootedPath(rooted))
		}
		return
	}
	_ = os.MkdirAll(c.dir, 0o755)
	// A partial entry does not decode and is treated as a miss, but removing it
	// keeps the cache directory clean.
	native := filepath.Native(path)
	f, err := os.OpenFile(native, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	err = json.MarshalWrite(f, ent)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(native)
	}
}
