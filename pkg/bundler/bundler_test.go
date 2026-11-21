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
	if strings.Contains(code, "__factories") || strings.Contains(code, "__esModule") || strings.Contains(code, "__importDefault") || strings.Contains(code, "require(") {
		t.Fatalf("bundle still uses a CommonJS runtime:\n%s", code)
	}
	abs, err := filepath.Abs(filepath.Join("testdata", "app", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, filepath.ToSlash(abs)) || strings.Contains(code, abs) {
		t.Fatalf("bundle contains an absolute path:\n%s", code)
	}
	if strings.Contains(code, `from "./`) || strings.Contains(code, `import("./`) {
		t.Fatalf("local modules were not inlined:\n%s", code)
	}
	if !strings.Contains(code, "React.createElement") {
		t.Fatal("tsx file was not transpiled to React.createElement")
	}
	if !strings.Contains(code, "export default") || !strings.Contains(code, "function add") || !strings.Contains(code, "export {") {
		t.Fatalf("bundle is not linked ESM:\n%s", code)
	}
	if strings.Contains(code, "drop-math") {
		t.Fatalf("unused export was not tree-shaken:\n%s", code)
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

func TestShadowedImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "c.ts"), []byte("export const n = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(`import { n } from "./c.js";
export function wrapped(): number {
  const n = 2;
  return n;
}
export function outer(): number {
  return n;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := Bundle(context.Background(), Options{Entry: filepath.Join(dir, "index.ts")})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	out := runNode(t, code, `
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
if (m.wrapped() !== 2 || m.outer() !== 1) {
  console.error(m.wrapped(), m.outer());
  process.exit(1);
}
console.log("ok");
`)
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("output %q", out)
	}
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
	abs, err := filepath.Abs(filepath.Join("testdata", "app", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, filepath.ToSlash(abs)) || strings.Contains(code, abs) {
		t.Fatalf("cjs bundle contains an absolute path:\n%s", code)
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
	if strings.Contains(code, "__factories") || strings.Contains(code, "__esModule") || strings.Contains(code, "require(") {
		t.Fatalf("bundle still uses a CommonJS runtime:\n%s", code)
	}
	abs, err := filepath.Abs(filepath.Join("testdata", "pkgapp", "src", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, filepath.ToSlash(abs)) || strings.Contains(code, "C:/") || strings.Contains(code, "C:\\") {
		t.Fatalf("bundle contains an absolute path:\n%s", code)
	}
	if strings.Contains(code, "drop-me") {
		t.Fatalf("unused package export was not tree-shaken:\n%s", code)
	}
	if !strings.Contains(code, `from "node:path"`) {
		t.Fatalf("node builtin was not kept as an ESM import:\n%s", code)
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

func TestAppDeclarationsAndSourceMap(t *testing.T) {
	result, err := Build(context.Background(), Options{
		Entry:   filepath.Join("testdata", "app", "index.ts"),
		OutFile: filepath.Join("dist", "app.mjs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	js := outputNamed(t, result.Files, "app.mjs")
	if !strings.Contains(js, "sourceMappingURL=app.mjs.map") {
		t.Fatalf("bundle missing source map comment:\n%s", js)
	}
	if strings.Contains(js, "drop-math") || strings.Contains(js, `from "./`) {
		t.Fatalf("app bundle changed shape:\n%s", js)
	}
	raw := outputNamed(t, result.Files, "app.mjs.map")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["version"] != float64(3) {
		t.Fatalf("map version = %v", decoded["version"])
	}
	sources, _ := decoded["sources"].([]any)
	var sawTS bool
	for _, source := range sources {
		name, _ := source.(string)
		if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx") {
			sawTS = true
		}
	}
	if !sawTS || decoded["mappings"] == "" {
		t.Fatalf("map sources = %v mappings = %v", sources, decoded["mappings"])
	}
	dts := outputNamed(t, result.Files, "app.d.ts")
	if !strings.Contains(dts, "read(): number") || !strings.Contains(dts, "function add") || !strings.Contains(dts, "function greet") {
		t.Fatalf("declaration missing entry surface:\n%s", dts)
	}
	if !strings.Contains(dts, "Ghost") || !strings.Contains(dts, "missing-types") {
		t.Fatalf("type-only import was dropped:\n%s", dts)
	}
}

func TestLibraryMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), `
export { add } from "./math";
import { secret } from "left-out";
export const leaked: string = secret;
export function load(): Promise<number> {
  return import("./dep").then((mod) => mod.value);
}
`)
	writeFile(t, filepath.Join(dir, "math.ts"), `
export function add(a: number, b: number): number { return a + b; }
`)
	writeFile(t, filepath.Join(dir, "dep.ts"), `
export const value = 7;
`)
	out := filepath.Join(dir, "dist")
	result, err := Build(context.Background(), Options{
		Entry:  filepath.Join(dir, "index.ts"),
		Mode:   ModeLib,
		OutDir: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	js := outputNamed(t, result.Files, "index.js")
	if !strings.Contains(js, `from "./math.js"`) || !strings.Contains(js, `from "left-out"`) || !strings.Contains(js, `import("./dep.js")`) {
		t.Fatalf("library entry did not keep module boundaries:\n%s", js)
	}
	if strings.Contains(js, "return a + b") {
		t.Fatalf("math was inlined into the entry:\n%s", js)
	}
	math := outputNamed(t, result.Files, "math.js")
	if !strings.Contains(math, "function add") {
		t.Fatalf("math.js:\n%s", math)
	}
	dts := outputNamed(t, result.Files, "math.d.ts")
	if !strings.Contains(dts, "add(a: number, b: number): number") {
		t.Fatalf("math.d.ts:\n%s", dts)
	}
	if !strings.Contains(outputNamed(t, result.Files, "index.d.ts"), `from "./math.js"`) {
		t.Fatalf("index.d.ts:\n%s", outputNamed(t, result.Files, "index.d.ts"))
	}
	raw := outputNamed(t, result.Files, "index.js.map")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["version"] != float64(3) || decoded["mappings"] == "" {
		t.Fatalf("library map = %s", raw)
	}
	for _, file := range result.Files {
		if !strings.HasPrefix(file.Path, out) {
			t.Fatalf("output %s is outside %s", file.Path, out)
		}
	}
}

func TestExternalPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), `
import { secret } from "left-out";
export const value = secret;
`)
	writeFile(t, filepath.Join(dir, "node_modules", "left-out", "package.json"), `{"type":"module","main":"index.js"}`)
	writeFile(t, filepath.Join(dir, "node_modules", "left-out", "index.js"), `export const secret = "BUNDLED_SECRET";`)
	code, err := Bundle(context.Background(), Options{
		Entry:    filepath.Join(dir, "index.ts"),
		External: []string{"left-out"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "BUNDLED_SECRET") || !strings.Contains(code, `from "left-out"`) {
		t.Fatalf("external package was bundled:\n%s", code)
	}
}

func TestSideEffectsFalse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"sideEffects":false}`)
	writeFile(t, filepath.Join(dir, "index.ts"), `
import "./effect.js";
export function value(): number { return 1; }
`)
	writeFile(t, filepath.Join(dir, "effect.ts"), `
console.log("SIDE_EFFECT_MARKER");
`)
	code, err := Bundle(context.Background(), Options{Entry: filepath.Join(dir, "index.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "SIDE_EFFECT_MARKER") {
		t.Fatalf("side-effect-free module was kept:\n%s", code)
	}
}

func outputNamed(t *testing.T, files []Output, name string) string {
	t.Helper()
	var data string
	n := 0
	for _, file := range files {
		if filepath.Base(file.Path) == name {
			data = file.Data
			n++
		}
	}
	if n != 1 {
		var names []string
		for _, file := range files {
			names = append(names, file.Path)
		}
		t.Fatalf("file %s matched %d outputs in %v", name, n, names)
	}
	return data
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
