package bundler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/core"
)

const minifiedPackage = `export function mega(a,b){return a+b}export const tiny={x:1,y:2};`

// TestLibDepsDir checks that --deps-dir moves bundled packages and rewrites their import specifiers.
func TestLibDepsDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "index.ts"), `import { helper } from "deep-pkg";
export const out = helper();
`)
	writeFile(t, filepath.Join(dir, "node_modules", "deep-pkg", "package.json"), `{"name":"deep-pkg","type":"module","main":"lib/index.js"}`)
	writeFile(t, filepath.Join(dir, "node_modules", "deep-pkg", "lib", "index.js"), `export { helper } from "./util/helper.js";`)
	writeFile(t, filepath.Join(dir, "node_modules", "deep-pkg", "lib", "util", "helper.js"), `export const helper = () => "help";`)
	out := filepath.Join(dir, "dist")
	result, err := Build(context.Background(), Options{
		Entries:           []string{filepath.Join(dir, "src", "index.ts")},
		Mode:              ModeLib,
		OutDir:            out,
		BundleNodeModules: true,
		DepsDir:           "vendor",
	})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range result.Files {
		files[filepath.ToSlash(file.Path)] = file.Data
	}
	depPath := filepath.ToSlash(filepath.Join(out, "vendor", "deep-pkg", "lib", "index.js"))
	if _, ok := files[depPath]; !ok {
		t.Fatalf("dependency was not written under vendor: %v", fileNames(files))
	}
	entry := files[filepath.ToSlash(filepath.Join(out, "src", "index.js"))]
	if !strings.Contains(entry, `from "../vendor/deep-pkg/lib/index.js"`) {
		t.Fatalf("import specifier does not follow the layout:\n%s", entry)
	}
}

func TestLibDepsDirRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), `export const a = 1;`)
	for _, value := range []string{"..", "../out", "/abs", "."} {
		_, err := Build(context.Background(), Options{
			Entries: []string{filepath.Join(dir, "index.ts")},
			Mode:    ModeLib,
			OutDir:  filepath.Join(dir, "dist"),
			DepsDir: value,
		})
		if err == nil || !strings.Contains(err.Error(), "deps dir") {
			t.Fatalf("DepsDir %q = %v, want an error", value, err)
		}
	}
}

func TestNodeModulesRel(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/repo/node_modules/pkg/lib/index.js", "pkg/lib/index.js", true},
		{"/repo/node_modules/.pnpm/pkg@1.0.0/node_modules/pkg/lib/index.js", "pkg/lib/index.js", true},
		{"/repo/src/index.ts", "", false},
		{"node_modules/pkg/index.js", "pkg/index.js", true},
		{"/repo/node_modules/pkg/index.js?v=1", "pkg/index.js", true},
		{"/repo/node_modules/pkg/index.js#hash", "pkg/index.js", true},
		{"C:/repo/node_modules/pkg/index.js", "pkg/index.js", true},
		{"/repo/NODE_MODULES/pkg/index.js", "pkg/index.js", true},
		{"/repo/node_modules", "", false},
		{"/repo/node_modules/", "", false},
		{"/repo/node_modulesX/pkg/index.js", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := modpath.NodeModulesRel(tc.path)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("NodeModulesRel(%q) = %q, %v; want %q, %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

// TestLibKeepsMinifiedDependency checks that a reprint-only dependency keeps its text and aligned map.
func TestLibKeepsMinifiedDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "index.ts"), `import { tiny } from "mini-pkg";
export const out = tiny.x;
`)
	writeFile(t, filepath.Join(dir, "node_modules", "mini-pkg", "package.json"), `{"name":"mini-pkg","type":"module","main":"index.js"}`)
	writeFile(t, filepath.Join(dir, "node_modules", "mini-pkg", "index.js"),
		"console.log(\"drop-me\");\n"+minifiedPackage+"\n")
	sourcemapFlag := true
	out := filepath.Join(dir, "dist")
	compiler := &core.CompilerOptions{}
	compiler.InlineSources = core.TSTrue
	result, err := Build(context.Background(), Options{
		Entries:           []string{filepath.Join(dir, "src", "index.ts")},
		Mode:              ModeLib,
		OutDir:            out,
		BundleNodeModules: true,
		SourceMap:         &sourcemapFlag,
		CompilerOptions:   compiler,
		Drop:              []string{"console"},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range result.Files {
		files[filepath.ToSlash(file.Path)] = file.Data
	}
	dep := files[filepath.ToSlash(filepath.Join(out, "node_modules", "mini-pkg", "index.js"))]
	if !strings.Contains(dep, minifiedPackage) {
		t.Fatalf("minified dependency was reformatted or not dropped:\n%s", dep)
	}
	if strings.Contains(dep, "drop-me") || strings.Contains(dep, "a, b") {
		t.Fatalf("minified dependency was not rewritten in place:\n%s", dep)
	}
	mapData := files[filepath.ToSlash(filepath.Join(out, "node_modules", "mini-pkg", "index.js.map"))]
	if mapData == "" {
		t.Fatal("verbatim dependency has no source map")
	}
	parsed := decodeTestSourceMap(t, mapData)
	assertMappedTo(t, dep, parsed, "const tiny", "export const tiny")
}

// TestAppBundleKeepsTreeShakingOfMinifiedDependency checks that minified text stays while unused exports and properties go.
func TestAppBundleKeepsTreeShakingOfMinifiedDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), `import { tiny } from "mini-pkg";
export const out = tiny.x;
`)
	writeFile(t, filepath.Join(dir, "node_modules", "mini-pkg", "package.json"), `{"name":"mini-pkg","type":"module","main":"index.js"}`)
	writeFile(t, filepath.Join(dir, "node_modules", "mini-pkg", "index.js"), minifiedPackage)
	code, err := Bundle(context.Background(), Options{
		Mode:    ModeApp,
		Entries: []string{filepath.Join(dir, "index.ts")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "mega") || strings.Contains(code, "y:2") {
		t.Fatalf("tree shaking did not run:\n%s", code)
	}
	if !strings.Contains(code, "const tiny={x:1};") {
		t.Fatalf("minified text was not preserved:\n%s", code)
	}
}

// TestAppBundleSeparatesStatements checks that bodies without a trailing semicolon do not swallow the next parenthesized body.
func TestAppBundleSeparatesStatements(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), `import { first } from "./first.js";
import "./second.js";
export const out = first;
`)
	writeFile(t, filepath.Join(dir, "first.js"), `export const first = 1`)
	writeFile(t, filepath.Join(dir, "second.js"), `(function () { globalThis.ran = true })()`)
	code, err := Bundle(context.Background(), Options{
		Mode:    ModeApp,
		Entries: []string{filepath.Join(dir, "index.ts")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "const first = 1;") {
		t.Fatalf("statement join was not terminated:\n%s", code)
	}
}

func fileNames(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
