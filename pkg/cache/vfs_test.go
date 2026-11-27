package cache

import (
	"testing"

	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs/vfstest"
)

func TestTranspileCacheWithVirtualFS(t *testing.T) {
	fsys := vfstest.FromMap(map[string]string{}, tspath.CaseSensitive)
	c := NewTranspileCache(fsys, "/cache")
	if c == nil {
		t.Fatal("nil cache")
	}
	want := Entry{JS: "const a = 1;", Map: `{"version":3}`}
	c.Put("key", want)

	got, ok := c.Get("key")
	if !ok || got != want {
		t.Fatalf("Get = %+v, %v; want %+v", got, ok, want)
	}
	text, ok := fsys.ReadFile(tspath.RootedFilePath("/cache/key.json"))
	if !ok || text == "" {
		t.Fatal("entry was not written to the injected fs")
	}
}

func TestTranspileCacheEmptyDirDisablesCache(t *testing.T) {
	if NewTranspileCache(nil, "") != nil {
		t.Fatal("an empty dir must disable the cache")
	}
}
