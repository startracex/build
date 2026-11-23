package bundler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func bundleDir(t *testing.T, dir, entry string) string {
	t.Helper()
	code, err := Bundle(context.Background(), Options{Mode: ModeApp, Entry: filepath.Join(dir, entry)})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	return code
}

// TestAppBundleKeepsDefaultParameterReferences guards a default value that
// reads a module binding: the parameter scope used to be walked without its
// initializers, so the binding looked unused and was dropped.
func TestAppBundleKeepsDefaultParameterReferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), `import { out } from "./main.js";
console.log(JSON.stringify({ out }));
`)
	writeFile(t, filepath.Join(dir, "main.js"), `import { SCOPE } from "./util.js";
function compile(name = SCOPE) { return name; }
export const out = compile();
`)
	writeFile(t, filepath.Join(dir, "util.js"), `export const SCOPE = "keyword";
`)
	code := bundleDir(t, dir, "index.js")
	if !strings.Contains(code, `"keyword"`) {
		t.Fatalf("default parameter reference was dropped:\n%s", code)
	}
	got := strings.TrimSpace(runNode(t, code, `const { pathToFileURL } = await import("node:url");
const m = await import(pathToFileURL(process.argv[2]).href);
console.log(JSON.stringify(m));`))
	if !strings.Contains(got, `"out":"keyword"`) {
		t.Fatalf("default parameter reference was dropped:\n%s\n%s", got, code)
	}
}

// TestAppBundleRenamesPropertyAccessObjects guards `x.prop` where x is a module
// local: the object reference was left unrenamed, so a second module owning the
// same source name made the access read the wrong binding.
func TestAppBundleRenamesPropertyAccessObjects(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), `import { a } from "./a.js";
import { b } from "./b.js";
console.log(JSON.stringify({ a, b }));
`)
	writeFile(t, filepath.Join(dir, "a.js"), `const shared = { __proto__: null };
shared.enumerable = true;
export const a = shared.enumerable;
`)
	writeFile(t, filepath.Join(dir, "b.js"), `const shared = { other: 1 };
shared.other = 2;
export const b = shared.other;
`)
	code := bundleDir(t, dir, "index.js")
	got := strings.TrimSpace(runNode(t, code, `const { pathToFileURL } = await import("node:url");
const m = await import(pathToFileURL(process.argv[2]).href);
console.log(JSON.stringify(m));`))
	if !strings.Contains(got, `"a":true`) || !strings.Contains(got, `"b":2`) {
		t.Fatalf("property access object was not renamed:\n%s\n%s", got, code)
	}
}

// TestAppBundleKeepsNamesOfUnusedSideEffectDeclarations guards a top-level
// declaration nothing references: the statement stays for its side effect, so
// its name still has to stay unique across modules.
func TestAppBundleKeepsNamesOfUnusedSideEffectDeclarations(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), `import { a } from "./a.js";
import { b } from "./b.js";
console.log(JSON.stringify({ a, b }));
`)
	writeFile(t, filepath.Join(dir, "a.js"), `import { track } from "./track.js";
const value = track("a");
export const a = 1;
`)
	writeFile(t, filepath.Join(dir, "b.js"), `import { track } from "./track.js";
const value = track("b");
export const b = 2;
`)
	writeFile(t, filepath.Join(dir, "track.js"), `export function track(name) { console.log("track " + name); return name; }
`)
	code := bundleDir(t, dir, "index.js")
	if strings.Count(code, "const value =") > 1 {
		t.Fatalf("duplicate top-level declaration:\n%s", code)
	}
	got := strings.TrimSpace(runNode(t, code, `const { pathToFileURL } = await import("node:url");
const m = await import(pathToFileURL(process.argv[2]).href);
console.log(JSON.stringify(m));`))
	if !strings.Contains(got, "track a") || !strings.Contains(got, "track b") {
		t.Fatalf("side effect declaration was dropped:\n%s\n%s", got, code)
	}
}

// TestAppBundleKeepsPropertiesReadThroughThis guards property-level tree
// shaking of an object literal: a method reading `this.x` keeps x alive even
// though no caller names it.
func TestAppBundleKeepsPropertiesReadThroughThis(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), `import { value } from "./store.js";
console.log(JSON.stringify({ value }));
`)
	writeFile(t, filepath.Join(dir, "store.js"), `const store = {
  entries: new Map([["k", 7]]),
  lookup(key) {
    return this.entries.get(key);
  },
};
export const value = store.lookup("k");
`)
	code := bundleDir(t, dir, "index.js")
	got := strings.TrimSpace(runNode(t, code, `const { pathToFileURL } = await import("node:url");
const m = await import(pathToFileURL(process.argv[2]).href);
console.log(JSON.stringify(m));`))
	if !strings.Contains(got, `"value":7`) {
		t.Fatalf("property read through this was dropped:\n%s\n%s", got, code)
	}
}
