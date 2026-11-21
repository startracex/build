package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if err := run([]string{"--outfile", out, "--entry", entry, "--format", "esm", "--jsx", "react"}, nil, nil); err != nil {
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

func TestCLIUsageErrors(t *testing.T) {
	if err := run(nil, nil, nil); err == nil || !strings.Contains(err.Error(), "missing entry") {
		t.Fatalf("error = %v", err)
	}
	if err := run([]string{"entry.ts"}, nil, nil); err == nil || !strings.Contains(err.Error(), "missing output") {
		t.Fatalf("error = %v", err)
	}
}
