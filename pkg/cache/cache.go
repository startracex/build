package cache

import (
	"encoding/json/v2"
	"os"
	"sync"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// TranspileCache stores transpile results keyed by content hash. The entries
// live under dir, on fs when one is set and on the OS file system otherwise.
type TranspileCache struct {
	fs  vfs.FS
	mu  sync.Mutex
	dir string
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
