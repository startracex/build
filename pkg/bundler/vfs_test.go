package bundler

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
	"github.com/startracex/tsgo/pkg/vfs/vfstest"
)

// countingFS records cache reads so a test can tell whether the second build hit the cache.
type countingFS struct {
	vfs.FS
	prefix     string
	cacheReads int
}

func (f *countingFS) ReadFile(path tspath.RootedFilePath) (string, bool) {
	if strings.HasPrefix(path.AsString(), f.prefix) {
		f.cacheReads++
	}
	return f.FS.ReadFile(path)
}

// virtualRoot returns a rooted path accepted on both Windows and POSIX hosts.
func virtualRoot(name string) string {
	if runtime.GOOS == "windows" {
		return "C:/" + name
	}
	return "/" + name
}

// TestBuildWithVirtualFS runs a build whose sources exist only in memory, so bypassing Options.FS fails.
func TestBuildWithVirtualFS(t *testing.T) {
	root := virtualRoot("virtual-build")
	fsys := vfstest.FromMap(map[string]string{
		root + "/src/index.ts": `import { label } from "./label";
export const value: string = label;
`,
		root + "/src/label.ts": `export const label = "virtual";
`,
	}, tspath.CaseSensitive)

	result, err := Build(context.Background(), Options{
		Mode:    ModeApp,
		Entry:   root + "/src/index.ts",
		OutFile: root + "/dist/out.js",
		FS:      fsys,
		CWD:     root,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	code := result.JavaScript()
	if !strings.Contains(code, `"virtual"`) {
		t.Fatalf("virtual module is missing from the bundle:\n%s", code)
	}
	if strings.Contains(code, `from "./`) {
		t.Fatalf("local import was not inlined:\n%s", code)
	}
	if got := result.Sources[root+"/src/index.ts"]; got == "" {
		t.Fatalf("sources = %v", result.Sources)
	}
}

// TestBuildVirtualFSRelativeEntry checks that a relative entry resolves against CWD, not the process directory.
func TestBuildVirtualFSRelativeEntry(t *testing.T) {
	root := virtualRoot("virtual-relative")
	fsys := vfstest.FromMap(map[string]string{
		root + "/src/index.ts": `export const value = 7;
`,
	}, tspath.CaseSensitive)

	result, err := Build(context.Background(), Options{
		Mode:    ModeApp,
		Entry:   "src/index.ts",
		OutFile: root + "/dist/out.js",
		FS:      fsys,
		CWD:     root,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(result.JavaScript(), "value = 7") {
		t.Fatalf("entry was not loaded:\n%s", result.JavaScript())
	}
}

// TestBuildVirtualFSCache checks that the transpile cache writes into the injected FS and is read back.
func TestBuildVirtualFSCache(t *testing.T) {
	root := virtualRoot("virtual-cache")
	fsys := &countingFS{
		FS: vfstest.FromMap(map[string]string{
			root + "/src/index.ts": `export const value = 7;
`,
		}, tspath.CaseSensitive),
		prefix: root + "/cache/",
	}
	opts := Options{
		Mode:     ModeApp,
		Entry:    root + "/src/index.ts",
		OutFile:  root + "/dist/out.js",
		FS:       fsys,
		CWD:      root,
		CacheDir: root + "/cache",
	}
	ctx := context.Background()

	first, err := Build(ctx, opts)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	if !strings.Contains(first.JavaScript(), "value = 7") {
		t.Fatalf("first bundle:\n%s", first.JavaScript())
	}
	readsAfterFirst := fsys.cacheReads

	second, err := Build(ctx, opts)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if fsys.cacheReads <= readsAfterFirst {
		t.Fatalf("second build did not read the cache (reads %d -> %d)", readsAfterFirst, fsys.cacheReads)
	}
	if !strings.Contains(second.JavaScript(), "value = 7") {
		t.Fatalf("second bundle:\n%s", second.JavaScript())
	}
}
