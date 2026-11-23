package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCLIWritesBundle(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist", "bundle.mjs")
	entry := filepath.Join("..", "..", "pkg", "bundler", "testdata", "app", "index.ts")
	var stdout, stderr strings.Builder
	if err := run([]string{"-o", out, entry}, &stderr, &stdout); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), out) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	script := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(script, []byte(`
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
if (m.add(2, 3) !== 5 || m.default("world") !== "hello world") {
  process.exit(1);
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, msg)
	}
}

func TestCLILongFlags(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "bundle.mjs")
	entry := filepath.Join("..", "..", "pkg", "bundler", "testdata", "app", "greet.ts")
	if err := run([]string{"--outfile", out, "--format", "esm", "--jsx", "react", entry}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "hello ") || !strings.Contains(string(body), "export default") {
		t.Fatalf("bundle missing ESM greet output:\n%s", body)
	}
}

func TestCLILibraryMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const product: number = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	var stdout strings.Builder
	if err := run([]string{"--map", "--dts", "--outdir", out, filepath.Join(src, "index.ts")}, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	indexPath := findOutput(t, out, "index.js")
	js, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `from "./math.js"`) {
		t.Fatalf("index.js:\n%s", js)
	}
	indexDir := filepath.Dir(indexPath)
	if _, err := os.Stat(filepath.Join(indexDir, "math.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(indexDir, "math.d.ts")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(indexDir, "math.d.ts.map")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(indexDir, "index.js.map")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "math.d.ts") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func findOutput(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == name {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("missing %s under %s", name, root)
	}
	return found
}

func TestCLIUsageErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := run(nil, nil, nil); err == nil || !strings.Contains(err.Error(), "tsconfig.json not found") {
		t.Fatalf("error = %v", err)
	}
	if err := run([]string{"--outfile", "out.js", "a.ts", "b.ts"}, nil, nil); err == nil || !strings.Contains(err.Error(), "single entry") {
		t.Fatalf("error = %v", err)
	}
	if err := run([]string{"--outfile", "out.js", "--outdir", "dist", "a.ts"}, nil, nil); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatalf("error = %v", err)
	}
	if err := run([]string{"-o", "out.js", "-o", "./out.js", "a.ts"}, nil, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v", err)
	}
	withConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(withConfig, "tsconfig.json"), []byte(`{"include":["a.ts"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withConfig, "a.ts"), []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(withConfig)
	if err := run([]string{"--outdir", "dist", "a.ts"}, nil, nil); err != nil {
		t.Fatalf("files on CLI should ignore tsconfig: %v", err)
	}
	if err := run([]string{"--project", withConfig, "a.ts"}, nil, nil); err != nil {
		t.Fatalf("project with explicit entry: %v", err)
	}
	nested := filepath.Join(withConfig, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if err := run([]string{"--project", ".."}, nil, nil); err != nil {
		t.Fatalf("project directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nested, "a.js")); err != nil {
		if _, err := os.Stat(filepath.Join(withConfig, "a.js")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLIUnsetTSConfigEmitFlags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("export const value = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{
  "compilerOptions": { "module": "esnext", "outDir": "dist" },
  "include": ["index.ts"]
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "index.js")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.js.map", "index.d.ts", "index.d.ts.map"} {
		if _, err := os.Stat(filepath.Join(dir, "dist", name)); err == nil {
			t.Fatalf("unexpected %s with unset emit flags", name)
		}
	}
}

func TestCLIGlobAndTSConfig(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const sum = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{
  "compilerOptions": { "module": "esnext", "declaration": true, "declarationMap": true, "sourceMap": true, "outDir": "dist", "rootDir": "src" },
  "include": ["src/**/*.ts"]
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var stdout strings.Builder
	if err := run(nil, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `from "./math.js"`) {
		t.Fatalf("index.js:\n%s", js)
	}
	dts, err := os.ReadFile(filepath.Join(dir, "dist", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dts), "sum: number") {
		t.Fatalf("index.d.ts:\n%s", dts)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "math.d.ts.map")); err != nil {
		t.Fatal(err)
	}

	globOut := filepath.Join(dir, "glob")
	if err := run([]string{"--map", "--dts", "--outdir", globOut, "src/**/*.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(globOut, "src", "math.js")); err != nil && !fileExists(filepath.Join(globOut, "math.js")) {
		t.Fatalf("glob output missing math.js: %v", err)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func TestCLIExplicitEmitFlagsOverrideTSConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("export const n: number = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"module":"esnext","sourceMap":true,"declaration":true,"declarationMap":true,"outDir":"tsout"},"include":["index.ts"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "clioff")
	if err := run([]string{"--map=false", "--dts", "--outdir", out}, nil, nil); err != nil {
		t.Fatal(err)
	}
	findOutput(t, out, "index.js")
	findOutput(t, out, "index.d.ts")
	if fileExists(filepath.Join(out, "index.js.map")) {
		t.Fatal("unexpected index.js.map with --map=false")
	}
	if fileExists(filepath.Join(out, "index.d.ts.map")) {
		t.Fatal("unexpected index.d.ts.map with --map=false")
	}
}

func TestCLITemplatedOutfileSingleEntry(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "index.ts")
	if err := os.WriteFile(entry, []byte("export const n: number = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pattern := filepath.Join(dir, "dist", "[name].js")
	if err := run([]string{"-o", pattern, entry}, nil, nil); err != nil {
		t.Fatal(err)
	}
	findOutput(t, filepath.Join(dir, "dist"), "index.js")
}

func TestCLITemplatedOutfileMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const sum = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := run([]string{"-o", filepath.Join("dist", "[name].cjs"), "--format", "cjs", "src"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.cjs", "math.cjs"} {
		findOutput(t, filepath.Join(dir, "dist"), name)
	}
}

func TestCLITemplatedOutfileHash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const sum = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	dist := filepath.Join(dir, "dist")
	if err := run([]string{"-o", filepath.Join("dist", "[name].[hash:6].cjs"), "--format", "cjs", "src"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dist)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^(index|math)\.[0-9a-f]{6}\.cjs$`)
	var indexName, mathName string
	for _, entry := range entries {
		if !pattern.MatchString(entry.Name()) {
			t.Fatalf("unexpected output %q", entry.Name())
		}
		if strings.HasPrefix(entry.Name(), "index.") {
			indexName = entry.Name()
		}
		if strings.HasPrefix(entry.Name(), "math.") {
			mathName = entry.Name()
		}
	}
	if indexName == "" || mathName == "" {
		t.Fatalf("missing hashed outputs: %#v", entries)
	}
	body, err := os.ReadFile(filepath.Join(dist, indexName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"./`+mathName+`"`) {
		t.Fatalf("index import not rewritten to hashed name %q:\n%s", mathName, body)
	}
}

func TestCLIEntryFileNamesWithOutdir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const sum = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	dist := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", "dist", "--entry-file-names", "[name].[hash:6].cjs", "--format", "cjs", "src"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dist)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^(index|math)\.[0-9a-f]{6}\.cjs$`)
	var indexName, mathName string
	for _, entry := range entries {
		if !pattern.MatchString(entry.Name()) {
			t.Fatalf("unexpected output %q", entry.Name())
		}
		if strings.HasPrefix(entry.Name(), "index.") {
			indexName = entry.Name()
		}
		if strings.HasPrefix(entry.Name(), "math.") {
			mathName = entry.Name()
		}
	}
	if indexName == "" || mathName == "" {
		t.Fatalf("missing outputs: %#v", entries)
	}
	body, err := os.ReadFile(filepath.Join(dist, indexName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"./`+mathName+`"`) {
		t.Fatalf("index import not rewritten to %q:\n%s", mathName, body)
	}

	literalOut := filepath.Join(dir, "app.js")
	if err := run([]string{"-o", literalOut, "--entry-file-names", "ignored.[name].js", filepath.Join(src, "index.ts")}, nil, nil); err != nil {
		t.Fatalf("entry-file-names should be ignored with --outfile: %v", err)
	}
	if _, err := os.Stat(literalOut); err != nil {
		t.Fatal(err)
	}
}

func TestCLIBundleNodeModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("import { secret } from \"pkg\";\nexport const value = secret;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","type":"module","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte("export const secret = \"BUNDLED_SECRET\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	externalOut := filepath.Join(dir, "external")
	if err := run([]string{"--outdir", externalOut, "index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(externalOut, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "from \"pkg\"") {
		t.Fatalf("expected package to stay external:\n%s", body)
	}

	bundledOut := filepath.Join(dir, "bundled")
	if err := run([]string{"--outdir", bundledOut, "--bundle-node-modules", "index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(bundledOut, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "from \"pkg\"") {
		t.Fatalf("package should be bundled:\n%s", body)
	}
	if !strings.Contains(string(body), "node_modules/pkg/index.js") || strings.Contains(string(body), "../../") {
		t.Fatalf("package import should point at the emitted file:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(bundledOut, "node_modules", "pkg", "index.js")); err != nil {
		t.Fatalf("bundled package file missing: %v", err)
	}
}

func TestCLIBundleNodeModulesCommonJSDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("import value from \"pkg/cjs.js\";\nexport const n = value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","type":"module","exports":{"./cjs.js":"./cjs.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "cjs.js"), []byte("module.exports = { value: 1 };\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", out, "--bundle-node-modules", "--entry-file-names", "[name].js", "index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	wrapped, err := os.ReadFile(filepath.Join(out, "cjs.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wrapped), "export default __cjs_default") {
		t.Fatalf("CommonJS module was not wrapped for ESM:\n%s", wrapped)
	}
	body, err := os.ReadFile(filepath.Join(out, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./cjs.js"`) {
		t.Fatalf("import not rewritten to wrapped module:\n%s", body)
	}
}

func TestCLIBundleNodeModulesMixedESMNotWrapped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("import value from \"pkg\";\nexport const n = value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","type":"module","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := "const typescript = { value: 1 };\nmodule.exports = typescript;\nexport { typescript as default };\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", out, "--bundle-node-modules", "--entry-file-names", "[name].js", "index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "index-2.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "__cjs_module") {
		t.Fatalf("mixed ESM/CJS module should not be wrapped:\n%s", body)
	}
}

func TestCLIBundleNodeModulesOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { secret } from \"pkg\";\nexport const value = secret;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","type":"module","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte("export const secret = \"BUNDLED_SECRET\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"module":"esnext","rootDir":"src"},"include":["src/**/*.ts"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"-p", ".", "--outdir", out, "--bundle-node-modules"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "../../") || !strings.Contains(string(body), "node_modules/pkg/index.js") {
		t.Fatalf("package import escaped output root or was not rewritten:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(out, "node_modules", "pkg", "index.js")); err != nil {
		t.Fatalf("bundled package file missing: %v", err)
	}
}

func TestCLIBundleNodeModulesJSDocTypedef(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("import { value } from \"pkg\";\nexport const n = value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"pkg","type":"module","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `/**
 * @typedef {object} Renderer
 * @property {(node: Node) => void} openNode
 */
/**
 * @typedef {{scope?: string, language?: string}} Node
 */
export const value = 1;
`
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", out, "--bundle-node-modules", "index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	findOutput(t, out, "index.js")
}

func TestCLIHTMLEntry(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html><body><div id="app"></div><script type="module" src="./main.ts"></script><link rel="stylesheet" href="./style.css"></body></html>`
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.ts"), []byte("import { value } from \"./util.js\";\ndocument.querySelector(\"#app\")!.textContent = value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "util.ts"), []byte("export const value = \"hello\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "style.css"), []byte("body { color: red; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", out, "src/index.html"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `src="./main.js"`) {
		t.Fatalf("script src not rewritten:\n%s", body)
	}
	if !strings.Contains(string(body), `href="./style.css"`) {
		t.Fatalf("stylesheet href not rewritten:\n%s", body)
	}
	css, err := os.ReadFile(filepath.Join(out, "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "color: red") {
		t.Fatalf("stylesheet not copied:\n%s", css)
	}
	findOutput(t, out, "main.js")
	findOutput(t, out, "util.js")
}

func TestCLIHTMLEntrySingleBundle(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html><body><script type="module" src="./main.ts"></script></body></html>`
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.ts"), []byte("import { value } from \"./util.js\";\nexport const n = value;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "util.ts"), []byte("export const value = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	bundle := filepath.Join(dir, "dist", "index.js")
	if err := run([]string{"-o", bundle, "src/index.html"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `src="./index.js"`) {
		t.Fatalf("script src not rewritten to single bundle:\n%s", body)
	}
	if _, err := os.Stat(bundle); err != nil {
		t.Fatal(err)
	}
}

func TestCLIEntryFileNamesPathAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	for _, sub := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(src, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "a", "index.ts"), []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b", "index.ts"), []byte("import { a } from \"../a/index.js\";\nexport const b = a + 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"module":"commonjs","rootDir":"src","outDir":"dist"},"include":["src/**/*.ts"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if err := run([]string{"-p", ".", "--outdir", "pathout", "--entry-file-names", "[path].cjs", "--format", "cjs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	pathOut := filepath.Join(dir, "pathout")
	body, err := os.ReadFile(filepath.Join(pathOut, "b", "index.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "../a/index.cjs") {
		t.Fatalf("path import not rewritten:\n%s", body)
	}

	if err := run([]string{"-p", ".", "--outdir", "nameout", "--entry-file-names", "[name].cjs", "--format", "cjs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	nameOut := filepath.Join(dir, "nameout")
	for _, name := range []string{"index.cjs", "index-2.cjs"} {
		if _, err := os.Stat(filepath.Join(nameOut, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	body, err = os.ReadFile(filepath.Join(nameOut, "index-2.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "./index.cjs") {
		t.Fatalf("renamed import not rewritten:\n%s", body)
	}
}

func TestCLIJSXPreserve(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "ui.tsx")
	if err := os.WriteFile(entry, []byte("export const view = <div className=\"x\">hi</div>;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"module":"esnext","jsx":"preserve","outDir":"tsout"},"include":["ui.tsx"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "tsout", "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<div") {
		t.Fatalf("preserve JSX missing from output:\n%s", body)
	}

	cliOut := filepath.Join(dir, "cliout")
	if err := run([]string{"--jsx", "preserve", "--outdir", cliOut, entry}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(cliOut, "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<div") {
		t.Fatalf("preserve JSX missing from CLI output:\n%s", body)
	}
}

func TestCLIProjectWithExplicitEntryUsesCompilerOptions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(src, "ui.tsx")
	if err := os.WriteFile(entry, []byte("export const view = <div>hi</div>;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"compilerOptions":{"jsx":"preserve","sourceMap":true,"declaration":true,"declarationMap":true,"outDir":"tsout","rootDir":"src"},"include":["src/**/*.tsx"]}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out := filepath.Join(dir, "out")
	if err := run([]string{"-p", ".", "--outdir", out, "src/ui.tsx"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<div") {
		t.Fatalf("jsx from tsconfig not applied:\n%s", body)
	}
	for _, name := range []string{"ui.js.map", "ui.d.ts", "ui.d.ts.map"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	bundle := filepath.Join(dir, "bundle.js")
	if err := run([]string{"-p", ".", "-o", bundle, "src/ui.tsx"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundle + ".map"); err != nil {
		t.Fatalf("outfile should use sourceMap from tsconfig: %v", err)
	}
}

func TestCLIDirectoryInput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.ts"), []byte("import { add } from \"./math.js\";\nexport const sum = add(2, 3);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "math.ts"), []byte("export function add(a: number, b: number): number { return a + b; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := filepath.Join(dir, "dist")
	if err := run([]string{"--outdir", out, "--format", "cjs", "src"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	findOutput(t, out, "index.js")
	findOutput(t, out, "math.js")
}

func TestCLIRightAlignedOutputs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(src, "index.ts")
	if err := os.WriteFile(entry, []byte("export const n: number = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "a")
	second := filepath.Join(dir, "b")
	third := filepath.Join(dir, "c")
	args := []string{
		"--outdir", first,
		"--outdir", second,
		"--outdir", third,
		"--format", "esm",
		"--format", "cjs",
		entry,
	}
	if err := run(args, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{first, second, third} {
		findOutput(t, out, "index.js")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCLITSConfigDecorators(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "function dec(target: any, key?: string) {}\nclass Service {\n  @dec\n  name: string = \"x\";\n}\nexport { Service };\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "target": "es2020",
    "experimentalDecorators": true,
    "emitDecoratorMetadata": true,
    "outDir": "dist"
  },
  "include": ["index.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "__decorate") {
		t.Fatalf("decorator was not transformed:\n%s", body)
	}
	if !strings.Contains(string(body), `__metadata("design:type", String)`) {
		t.Fatalf("decorator metadata was not emitted:\n%s", body)
	}
}

func TestCLITSConfigImportHelpers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "function dec(t: any) {}\nclass S {\n  @dec\n  name = \"x\";\n}\nexport { S };\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "target": "es2020",
    "experimentalDecorators": true,
    "emitDecoratorMetadata": true,
    "importHelpers": true,
    "outDir": "dist"
  },
  "include": ["index.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "tslib"`) {
		t.Fatalf("importHelpers should import the helper from tslib:\n%s", body)
	}
	if strings.Contains(string(body), "var __decorate") {
		t.Fatalf("importHelpers should not inline the helper:\n%s", body)
	}

	cjsOut := filepath.Join(dir, "cjs")
	if err := run([]string{"-p", ".", "--outdir", cjsOut, "--format", "cjs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(cjsOut, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `require("tslib")`) {
		t.Fatalf("CommonJS output should require tslib:\n%s", body)
	}
}

func TestCLITSConfigUseDefineForClassFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "export class Counter { count = 0; }\n")
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "outDir": "define", "useDefineForClassFields": true },
  "include": ["index.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "define", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "this.count = 0") {
		t.Fatalf("useDefineForClassFields=true should keep the native field:\n%s", body)
	}
	if !strings.Contains(string(body), "count = 0;") {
		t.Fatalf("native class field missing:\n%s", body)
	}

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "outDir": "assign", "useDefineForClassFields": false },
  "include": ["index.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "assign", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "this.count = 0") {
		t.Fatalf("useDefineForClassFields=false should assign in the constructor:\n%s", body)
	}
}

func TestCLITSConfigJSXOptions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ui.tsx"), "export const view = <div>hi</div>;\n")
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "jsx": "react",
    "jsxFactory": "h",
    "jsxFragmentFactory": "Fragment",
    "outDir": "factory"
  },
  "include": ["ui.tsx"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "factory", "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `h("div", null, "hi")`) {
		t.Fatalf("jsxFactory from tsconfig not applied:\n%s", body)
	}
	if strings.Contains(string(body), "React.createElement") {
		t.Fatalf("jsxFactory should replace the default factory:\n%s", body)
	}

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "jsx": "react-jsx",
    "jsxImportSource": "preact",
    "outDir": "source"
  },
  "include": ["ui.tsx"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "source", "ui.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "preact/jsx-runtime"`) {
		t.Fatalf("jsxImportSource from tsconfig not applied:\n%s", body)
	}
}

func TestCLITSConfigPaths(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "index.ts"), "import { value } from \"@app/util.js\";\nexport const n = value;\n")
	writeFile(t, filepath.Join(src, "util.ts"), "export const value = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "baseUrl": ".",
    "paths": { "@app/*": ["src/*"] },
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./util.js"`) {
		t.Fatalf("paths alias was not resolved:\n%s", body)
	}
}

func TestCLITSConfigPathsWithBaseUrlSubdirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "index.ts"), "import { value } from \"@app/util\";\nexport const n = value;\n")
	writeFile(t, filepath.Join(src, "util.ts"), "export const value = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "baseUrl": "./src",
    "paths": { "@app/*": ["*"] },
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./util.js"`) {
		t.Fatalf("paths alias was not resolved against baseUrl:\n%s", body)
	}
}

func TestCLITSConfigBaseUrlFallsBackToProjectRoot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "index.ts"), "import { value } from \"helper\";\nexport const n = value;\n")
	writeFile(t, filepath.Join(src, "helper.ts"), "export const value = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "baseUrl": "./src",
    "declaration": true,
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./helper.js"`) {
		t.Fatalf("bare specifier was not resolved against baseUrl:\n%s", body)
	}
	decl, err := os.ReadFile(filepath.Join(dir, "dist", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decl), `export declare const n`) {
		t.Fatalf("declaration did not resolve the bare specifier against baseUrl:\n%s", decl)
	}
}

func TestCLITSConfigBaseUrlKeepsPackagesExternal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	pkg := filepath.Join(dir, "node_modules", "some-pkg")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "index.ts"), "import { value } from \"some-pkg\";\nexport const n = value;\n")
	writeFile(t, filepath.Join(pkg, "index.js"), "export const value = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "baseUrl": "./src",
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "some-pkg"`) {
		t.Fatalf("package import should stay external:\n%s", body)
	}
	if strings.Contains(string(body), "const value = 1") {
		t.Fatalf("package import should not be bundled:\n%s", body)
	}
}

func TestCLITSConfigPathsWithoutBaseUrl(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "index.ts"), "import { value } from \"@app/util\";\nexport const n = value;\n")
	writeFile(t, filepath.Join(src, "util.ts"), "export const value = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "paths": { "@app/*": ["./src/*"] },
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./util.js"`) {
		t.Fatalf("paths alias was not resolved:\n%s", body)
	}

	bundle := filepath.Join(dir, "bundle.js")
	if err := run([]string{"-p", ".", "-o", bundle, "src/index.ts"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "@app/util") {
		t.Fatalf("paths alias was not bundled:\n%s", body)
	}
	if !strings.Contains(string(body), "const value = 1") {
		t.Fatalf("aliased module missing from the bundle:\n%s", body)
	}
}

func TestCLITSConfigRemovedOptions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "export const n: number = 1;\n")
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "module": "esnext",
    "esModuleInterop": false,
    "allowSyntheticDefaultImports": false,
    "moduleResolution": "node",
    "outDir": "dist"
  },
  "include": ["index.ts"]
}
`)
	t.Chdir(dir)
	if err := run(nil, nil, nil); err != nil {
		t.Fatalf("removed TypeScript options should not fail the build: %v", err)
	}
	findOutput(t, filepath.Join(dir, "dist"), "index.js")
}

func TestCLITSConfigVerbatimModuleSyntax(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "import * as ns from \"./dep.js\";\nexport const n = 1;\n")
	writeFile(t, filepath.Join(dir, "dep.ts"), "export const value = 1;\n")
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "outDir": "elided" },
  "include": ["*.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "elided", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `from "./dep.js"`) {
		t.Fatalf("unused import should be elided without verbatimModuleSyntax:\n%s", body)
	}

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "verbatimModuleSyntax": true, "outDir": "verbatim" },
  "include": ["*.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "verbatim", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `from "./dep.js"`) {
		t.Fatalf("verbatimModuleSyntax should keep the import:\n%s", body)
	}
}

func TestCLITSConfigDeclarationCompilerOptions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "export const p = Promise.resolve(1);\n")
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "lib": ["es2015"], "declaration": true, "outDir": "modern" },
  "include": ["index.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "modern", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Promise<number>") {
		t.Fatalf("lib from tsconfig not applied to the declaration program:\n%s", body)
	}

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "lib": ["es5"], "declaration": true, "outDir": "legacy" },
  "include": ["index.ts"]
}
`)
	if err := run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "legacy", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Promise<number>") {
		t.Fatalf("lib es5 should not know Promise:\n%s", body)
	}
}

func TestCLITSConfigModuleResolution(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), "import { value } from \"pkg\";\nexport const n = value;\n")
	pkgDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The "node" export condition only exists for the node resolution kinds.
	writeFile(t, filepath.Join(pkgDir, "package.json"), `{"name":"pkg","type":"module","exports":{".":{"node":"./node.js"}}}`)
	writeFile(t, filepath.Join(pkgDir, "node.js"), "export const value = \"NODE_BUILD\";\n")
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "moduleResolution": "node16", "outDir": "dist" },
  "include": ["index.ts"]
}
`)
	bundle := filepath.Join(dir, "bundle.js")
	if err := run([]string{"-o", bundle}, nil, nil); err != nil {
		t.Fatalf("moduleResolution node16 should match the node condition: %v", err)
	}
	body, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "NODE_BUILD") {
		t.Fatalf("package was not bundled:\n%s", body)
	}

	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": { "module": "esnext", "outDir": "dist" },
  "include": ["index.ts"]
}
`)
	err = run([]string{"-o", filepath.Join(dir, "bundler.js")}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "pkg") {
		t.Fatalf("bundler resolution should not match the node condition: %v", err)
	}
}
