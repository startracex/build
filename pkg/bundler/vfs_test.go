package bundler

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs/vfstest"
)

// virtualRoot returns a rooted path the current platform accepts, so the same
// in-memory tree works on Windows and on POSIX hosts.
func virtualRoot(name string) string {
	if runtime.GOOS == "windows" {
		return "C:/" + name
	}
	return "/" + name
}

// TestBuildWithVirtualFS runs a build whose sources exist only in memory. No
// path in the tree exists on disk, so a read that bypasses Options.FS fails.
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

// TestBuildVirtualFSRelativeEntry checks that a relative entry is resolved
// against the injected working directory instead of the process CWD.
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
