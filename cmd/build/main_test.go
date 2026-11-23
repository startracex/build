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
	if err := run([]string{"--project", withConfig, "a.ts"}, nil, nil); err == nil || !strings.Contains(err.Error(), "cannot be mixed") {
		t.Fatalf("error = %v", err)
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
