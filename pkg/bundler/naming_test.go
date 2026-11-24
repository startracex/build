package bundler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestEntryFileNamesInAppSplit checks that EntryFileNames names the entry files
// of a split (multi-entry) app build, a layout that used to ignore it.
func TestEntryFileNamesInAppSplit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "a.ts"), `export const a = "A";`)
	writeFile(t, filepath.Join(dir, "src", "b.ts"), `export const b = "B";`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entries:        []string{filepath.Join(dir, "src", "a.ts"), filepath.Join(dir, "src", "b.ts")},
		Mode:           ModeApp,
		OutDir:         out,
		EntryFileNames: "entries/[name]-[hash:6][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	entriesPrefix := filepath.ToSlash(filepath.Join(out, "entries")) + "/"
	for _, f := range result.Files {
		p := filepath.ToSlash(f.Path)
		if strings.HasPrefix(p, entriesPrefix) && strings.HasSuffix(p, ".js") {
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("entry pattern not applied to both entries: %v", pathsOf(result.Files))
	}
}

// TestChunkFileNamesForAsyncChunk checks that ChunkFileNames controls the
// dynamic-import chunk path and that the entry imports it with a relative path.
func TestChunkFileNamesForAsyncChunk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lazy.ts"), `export const lazy = "lazy";`)
	writeFile(t, filepath.Join(dir, "index.ts"), `
export async function load() {
  const m = await import("./lazy");
  return m.lazy;
}
`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(dir, "index.ts"),
		Mode:           ModeApp,
		OutDir:         out,
		ChunkFileNames: "js/[name]-[hash:6][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	var chunkBase string
	for _, f := range result.Files {
		p := filepath.ToSlash(f.Path)
		if strings.Contains(p, "/js/lazy-") && strings.HasSuffix(p, ".js") {
			chunkBase = filepath.Base(p)
		}
	}
	if chunkBase == "" {
		t.Fatalf("chunk pattern not applied: %v", pathsOf(result.Files))
	}
	entry := outputNamed(t, result.Files, "index.js")
	if !strings.Contains(entry, `import("./js/`+chunkBase+`")`) {
		t.Fatalf("entry does not import the chunk relative to its directory:\n%s", entry)
	}
}

// TestChunkFileNamesForManualChunk checks that a manual chunk also follows the
// pattern, not only automatic shared/async chunks.
func TestChunkFileNamesForManualChunk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "vendor.ts"), `export const vend = "VENDOR";`)
	writeFile(t, filepath.Join(dir, "a.ts"), `import { vend } from "./vendor"; export const a = vend + "-a";`)
	writeFile(t, filepath.Join(dir, "b.ts"), `import { vend } from "./vendor"; export const b = vend + "-b";`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entries:        []string{filepath.Join(dir, "a.ts"), filepath.Join(dir, "b.ts")},
		Mode:           ModeApp,
		OutDir:         out,
		ChunkFileNames: "parts/[name][extname]",
		ManualChunks:   map[string]string{"vendor": "vendor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range result.Files {
		if filepath.ToSlash(f.Path) == filepath.ToSlash(filepath.Join(out, "parts", "vendor.js")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("manual chunk pattern not applied: %v", pathsOf(result.Files))
	}
}

// TestAssetFileNamesForURL checks that ?url assets use AssetFileNames and the
// emitted module points at the same path.
func TestAssetFileNamesForURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "note.txt"), "hello")
	writeFile(t, filepath.Join(dir, "index.ts"), `
import url from "./note.txt?url";
export { url };
`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(dir, "index.ts"),
		Mode:           ModeApp,
		OutDir:         out,
		AssetFileNames: "media/[name]-[hash:6][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range result.Files {
		p := filepath.ToSlash(f.Path)
		if strings.Contains(p, "/media/note-") && strings.HasSuffix(p, ".txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("asset pattern not applied: %v", pathsOf(result.Files))
	}
	if js := result.JavaScript(); !strings.Contains(js, "./media/note-") {
		t.Fatalf("asset URL not rewritten to media/:\n%s", js)
	}
}

// TestAssetFileNamesForEntryCSS checks that entry CSS uses AssetFileNames and is
// written exactly once.
func TestAssetFileNamesForEntryCSS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "style.css"), ".x { color: red; }")
	writeFile(t, filepath.Join(dir, "index.ts"), `import "./style.css"; export const a = 1;`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(dir, "index.ts"),
		Mode:           ModeApp,
		OutDir:         out,
		AssetFileNames: "css/[name]-[hash:6][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, f := range result.Files {
		p := filepath.ToSlash(f.Path)
		if strings.Contains(p, "/css/index-") && strings.HasSuffix(p, ".css") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one CSS at css/index-*, got %d: %v", count, pathsOf(result.Files))
	}
}

// TestTypePlaceholder checks that [type] expands to entry, chunk, or asset
// depending on what the pattern names.
func TestTypePlaceholder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lazy.ts"), `export const lazy = "lazy";`)
	writeFile(t, filepath.Join(dir, "note.txt"), "hello")
	writeFile(t, filepath.Join(dir, "index.ts"), `import url from "./note.txt?url";
export const load = () => import("./lazy.js");
export { url };
`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(dir, "index.ts"),
		Mode:           ModeApp,
		OutDir:         out,
		EntryFileNames: "[type]/[name][extname]",
		ChunkFileNames: "[type]/[name]-[hash:6][extname]",
		AssetFileNames: "[type]/[name]-[hash:6][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, f := range result.Files {
		paths[filepath.ToSlash(f.Path)] = true
	}
	if !paths[filepath.ToSlash(filepath.Join(out, "entry", "index.js"))] {
		t.Fatalf("entry type not applied: %v", pathsOf(result.Files))
	}
	var chunkOK, assetOK bool
	for p := range paths {
		if strings.Contains(p, "/chunk/lazy-") {
			chunkOK = true
		}
		if strings.Contains(p, "/asset/note-") {
			assetOK = true
		}
	}
	if !chunkOK || !assetOK {
		t.Fatalf("type not applied to chunk/asset: %v", pathsOf(result.Files))
	}
}

// TestEntryFileNamesInLibWithExt checks the [format]/[extname] placeholders in
// library mode.
func TestEntryFileNamesInLibWithExt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "index.ts"), `export const value = 1;`)
	out := filepath.Join(dir, "dist")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(dir, "src", "index.ts"),
		Mode:           ModeLib,
		OutDir:         out,
		RootDir:        filepath.Join(dir, "src"),
		EntryFileNames: "[name].[format][extname]",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(out, "index.esm.js"))
	for _, f := range result.Files {
		if filepath.ToSlash(f.Path) == want {
			return
		}
	}
	t.Fatalf("entry pattern with [format]/[extname] not applied, want %s: %v", want, pathsOf(result.Files))
}
