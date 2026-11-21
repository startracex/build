package bundler

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureBundle(t *testing.T) {
	code, err := Bundle(context.Background(), Options{Entry: filepath.Join("testdata", "app", "index.ts")})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if strings.Contains(code, `require("./`) || strings.Contains(code, `require("../`) {
		t.Fatalf("relative require was left unlinked:\n%s", code)
	}
	if !strings.Contains(code, "React.createElement") {
		t.Fatal("tsx file was not transpiled to React.createElement")
	}
	if !strings.Contains(code, "export default") || !strings.Contains(code, "export const add") {
		t.Fatal("bundle is not ESM")
	}

	out := runBundle(t, code)
	if out.Add != 5 || out.Product != 8 || out.Greet != "hello world" || out.Default != "hello world" {
		t.Fatalf("exports = %+v", out)
	}
	if out.Label != "bundled" || out.Banner != "ok" || out.Note != "indexed" {
		t.Fatalf("linked modules = %+v", out)
	}
	if out.Hello != "function" {
		t.Fatalf("Hello = %q", out.Hello)
	}
	if out.Read != 2 {
		t.Fatalf("live binding read() = %d, want 2", out.Read)
	}
	if out.Load != 7 {
		t.Fatalf("dynamic import load() = %d, want 7", out.Load)
	}
}

func TestImportCycle(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.ts")
	b := filepath.Join(dir, "b.ts")
	if err := os.WriteFile(a, []byte(`import { fromA } from "./b.js";
export const mark = "A";
export function both(): string { return mark + fromA(); }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`import { mark } from "./a.js";
export function fromA(): string { return mark; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Bundle(context.Background(), Options{Entry: a})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	dir = t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.mjs")
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(bundlePath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(`
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
if (m.both() !== "AA") process.exit(1);
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", scriptPath, bundlePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
}

func TestJSXAutomatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "react"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui.tsx"), []byte(`
export function Hello(name: string) {
  return <span className="hi">{name}</span>;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "react", "package.json"), []byte(`{
  "name": "react",
  "exports": { "./jsx-runtime": "./jsx-runtime.js" }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "react", "jsx-runtime.js"), []byte(`
export function jsx(type, props) { return { type, props }; }
export function jsxs(type, props) { return { type, props }; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Bundle(context.Background(), Options{Entry: filepath.Join(dir, "ui.tsx"), JSX: JSXAutomatic})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if strings.Contains(code, `require("react/jsx-runtime")`) {
		t.Fatal("jsx runtime was left external")
	}
	out := runNode(t, code, `
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
const el = m.Hello("Ada");
if (!el || el.type !== "span" || !el.props || el.props.className !== "hi") {
  console.error(JSON.stringify(el));
  process.exit(1);
}
console.log("ok");
`)
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("output %q", out)
	}
}

func TestMissingImport(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "index.ts")
	if err := os.WriteFile(entry, []byte(`import { missing } from "./missing.js";
export const value = missing;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Bundle(context.Background(), Options{Entry: entry})
	if err == nil || !strings.Contains(err.Error(), "missing.js") {
		t.Fatalf("error = %v", err)
	}
}

func TestSyntaxError(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "broken.ts")
	if err := os.WriteFile(entry, []byte("export const x: = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Bundle(context.Background(), Options{Entry: entry})
	if err == nil || !strings.Contains(err.Error(), "TS") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveHook(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "index.ts")
	other := filepath.Join(dir, "other.ts")
	if err := os.WriteFile(entry, []byte(`import { thing } from "alias:thing";
export const value = thing;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("export const thing = 11;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Bundle(context.Background(), Options{
		Entry: entry,
		Resolve: func(_, spec string) (string, bool, error) {
			if spec == "alias:thing" {
				return other, true, nil
			}
			return "", false, nil
		},
	})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	out := runBundle(t, code)
	if out.Value != 11 {
		t.Fatalf("value = %d", out.Value)
	}
}

type bundleResult struct {
	Add     int    `json:"add"`
	Greet   string `json:"greet"`
	Default string `json:"default"`
	Label   string `json:"label"`
	Product int    `json:"product"`
	Banner  string `json:"banner"`
	Hello   string `json:"hello"`
	Note    string `json:"note"`
	Read    int    `json:"read"`
	Load    int    `json:"load"`
	Value   int    `json:"value"`
}

func TestCJSFormat(t *testing.T) {
	code, err := Bundle(context.Background(), Options{
		Entry:  filepath.Join("testdata", "app", "index.ts"),
		Format: FormatCJS,
	})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if strings.Contains(code, "export default") {
		t.Fatal("cjs bundle contains an ESM export")
	}
	out := runCJS(t, code)
	if out.Add != 5 || out.Default != "hello world" || out.Read != 2 {
		t.Fatalf("exports = %+v", out)
	}
}

func TestPackageResolution(t *testing.T) {
	code, err := Bundle(context.Background(), Options{Entry: filepath.Join("testdata", "pkgapp", "src", "index.ts")})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if strings.Contains(code, `require("pkg-module")`) || strings.Contains(code, `require("pkg-exports")`) {
		t.Fatal("bare specifier was left external")
	}
	if !strings.Contains(code, `from "node:path"`) {
		t.Fatal("node builtin was not kept as an ESM import")
	}
	raw := runNode(t, code, `
import { pathToFileURL } from "node:url";
import { sep } from "node:path";
const m = await import(pathToFileURL(process.argv[2]).href);
console.log(JSON.stringify({
  fromMain: m.fromMain,
  fromModule: m.fromModule,
  fromExports: m.fromExports,
  fromSub: m.fromSub,
  title: m.title,
  sep
}));
`)
	var got struct {
		FromMain    string `json:"fromMain"`
		FromModule  string `json:"fromModule"`
		FromExports string `json:"fromExports"`
		FromSub     string `json:"fromSub"`
		Title       string `json:"title"`
		Sep         string `json:"sep"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if got.FromMain != "main"+got.Sep || got.FromModule != "from-module" || got.FromExports != "exports-import" || got.FromSub != "sub" || got.Title != "app" {
		t.Fatalf("resolved exports = %+v", got)
	}

	cjs, err := Bundle(context.Background(), Options{
		Entry:  filepath.Join("testdata", "pkgapp", "src", "index.ts"),
		Format: FormatCJS,
	})
	if err != nil {
		t.Fatalf("cjs bundle: %v", err)
	}
	cjsRaw := runNodeExt(t, cjs, "cjs", `
const { sep } = require("node:path");
const m = require(process.argv[2]);
console.log(JSON.stringify({
  fromModule: m.fromModule,
  fromExports: m.fromExports,
  fromMain: m.fromMain,
  sep
}));
`)
	var cjsGot struct {
		FromModule  string `json:"fromModule"`
		FromExports string `json:"fromExports"`
		FromMain    string `json:"fromMain"`
		Sep         string `json:"sep"`
	}
	if err := json.Unmarshal([]byte(cjsRaw), &cjsGot); err != nil {
		t.Fatalf("decode %q: %v", cjsRaw, err)
	}
	if cjsGot.FromModule != "from-main" || cjsGot.FromExports != "exports-require" || cjsGot.FromMain != "main"+cjsGot.Sep {
		t.Fatalf("cjs resolution = %+v", cjsGot)
	}
}

func TestCSSUnsupported(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "index.ts")
	if err := os.WriteFile(entry, []byte("import \"./app.css\";\nexport const ok = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Bundle(context.Background(), Options{Entry: entry})
	if err == nil || !strings.Contains(err.Error(), "CSS") {
		t.Fatalf("error = %v", err)
	}
}

func runBundle(t *testing.T, code string) bundleResult {
	t.Helper()
	raw := runNode(t, code, `
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
const load = typeof m.load === "function" ? await m.load() : 0;
console.log(JSON.stringify({
  add: m.add ? m.add(2, 3) : 0,
  greet: m.greet ? m.greet("world") : "",
  default: typeof m.default === "function" ? m.default("world") : "",
  label: m.label || "",
  product: m.product || 0,
  banner: m.banner || "",
  hello: typeof m.Hello,
  note: m.note || "",
  read: m.read ? m.read() : 0,
  load,
  value: m.value || 0,
}));
`)
	var result bundleResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return result
}

func runCJS(t *testing.T, code string) bundleResult {
	t.Helper()
	raw := runNodeExt(t, code, "cjs", `
const m = require(process.argv[2]);
Promise.resolve(typeof m.load === "function" ? m.load() : 0).then((load) => {
  console.log(JSON.stringify({
    add: m.add ? m.add(2, 3) : 0,
    greet: m.greet ? m.greet("world") : "",
    default: typeof m.default === "function" ? m.default("world") : "",
    label: m.label || "",
    product: m.product || 0,
    banner: m.banner || "",
    hello: typeof m.Hello,
    note: m.note || "",
    read: m.read ? m.read() : 0,
    load: load || 0,
    value: m.value || 0,
  }));
}).catch((err) => { console.error(err); process.exit(1); });
`)
	var result bundleResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return result
}

func runNode(t *testing.T, code, script string) string {
	t.Helper()
	return runNodeExt(t, code, "esm", script)
}

func runNodeExt(t *testing.T, code, kind, script string) string {
	t.Helper()
	dir := t.TempDir()
	ext := ".mjs"
	scriptExt := ".mjs"
	if kind == "cjs" {
		ext = ".cjs"
		scriptExt = ".cjs"
	}
	bundlePath := filepath.Join(dir, "bundle"+ext)
	scriptPath := filepath.Join(dir, "check"+scriptExt)
	if err := os.WriteFile(bundlePath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", scriptPath, bundlePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return string(out)
}
