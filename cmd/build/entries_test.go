package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpandEntriesDirectory(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		filepath.Join("src", "index.ts"):                    "export const a = 1;\n",
		filepath.Join("src", "util.ts"):                     "export const b = 2;\n",
		filepath.Join("src", "tests", "skip.ts"):            "export const c = 3;\n",
		filepath.Join("src", "types.d.ts"):                  "declare const d: number;\n",
		filepath.Join("src", "readme.md"):                   "ignored\n",
		filepath.Join("src", "node_modules", "pkg", "m.js"): "module.exports = {};\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	got, err := expandEntries([]string{"src"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "src", "index.ts"),
		filepath.Join(dir, "src", "tests", "skip.ts"),
		filepath.Join(dir, "src", "util.ts"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}

	got, err = expandEntries([]string{"src", "!src/tests/**"})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{
		filepath.Join(dir, "src", "index.ts"),
		filepath.Join(dir, "src", "util.ts"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries with exclusion = %#v, want %#v", got, want)
	}
}

func TestExpandEntriesDirectoryWithoutSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if _, err := expandEntries([]string{"src"}); err == nil || !strings.Contains(err.Error(), "no source files found") {
		t.Fatalf("error = %v", err)
	}
}
