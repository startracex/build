package bundler

import (
	"context"
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tspath"
)

// TestLibrarySourceMapSourcesMatchTSC checks that lib maps write "sources" relative to the .map file, as tsc does.
func TestLibrarySourceMapSourcesMatchTSC(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "src")
	writeFile(t, filepath.Join(sourceDir, "index.ts"), "export const value = 1;\n")

	result, err := Build(context.Background(), Options{
		Entry:          filepath.Join(sourceDir, "index.ts"),
		Mode:           ModeLib,
		OutDir:         filepath.Join(dir, "dist"),
		RootDir:        sourceDir,
		SourceMap:      new(true),
		Declaration:    new(true),
		DeclarationMap: new(true),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"index.js.map", "index.d.ts.map"} {
		raw := outputNamed(t, result.Files, name)
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if decoded["sourceRoot"] != "" {
			t.Errorf("%s sourceRoot = %v, want empty string", name, decoded["sourceRoot"])
		}
		sources, _ := decoded["sources"].([]any)
		if len(sources) != 1 || sources[0] != "../src/index.ts" {
			t.Errorf("%s sources = %v, want [../src/index.ts]", name, sources)
		}
	}
}

// TestLibrarySourceMapConfigFromTSConfig checks that sourceRoot and inlineSources reach emitted maps.
func TestLibrarySourceMapConfigFromTSConfig(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "src")
	source := "export const value = 1;\n"
	writeFile(t, filepath.Join(sourceDir, "index.ts"), source)

	compiler := &core.CompilerOptions{}
	compiler.SourceRoot = tspath.ToSourceMapLocation("https://example.com/src")
	compiler.InlineSources = core.TSTrue

	result, err := Build(context.Background(), Options{
		Entry:           filepath.Join(sourceDir, "index.ts"),
		Mode:            ModeLib,
		OutDir:          filepath.Join(dir, "dist"),
		RootDir:         sourceDir,
		SourceMap:       new(true),
		Declaration:     new(true),
		DeclarationMap:  new(true),
		CompilerOptions: compiler,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"index.js.map", "index.d.ts.map"} {
		raw := outputNamed(t, result.Files, name)
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := decoded["sourceRoot"]; got != "https://example.com/src" {
			t.Errorf("%s sourceRoot = %v", name, got)
		}
		contents, _ := decoded["sourcesContent"].([]any)
		if len(contents) != 1 || contents[0] != source {
			t.Errorf("%s sourcesContent = %v, want [%q]", name, contents, source)
		}
	}
}

// TestLibrarySourceMapNoInlineSources checks that inlineSources=false removes sourcesContent, matching tsc.
func TestLibrarySourceMapNoInlineSources(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "src")
	writeFile(t, filepath.Join(sourceDir, "index.ts"), "export const value = 1;\n")

	compiler := &core.CompilerOptions{}
	compiler.InlineSources = core.TSFalse

	result, err := Build(context.Background(), Options{
		Entry:           filepath.Join(sourceDir, "index.ts"),
		Mode:            ModeLib,
		OutDir:          filepath.Join(dir, "dist"),
		RootDir:         sourceDir,
		SourceMap:       new(true),
		Declaration:     new(true),
		DeclarationMap:  new(true),
		CompilerOptions: compiler,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"index.js.map", "index.d.ts.map"} {
		raw := outputNamed(t, result.Files, name)
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := decoded["sourcesContent"]; ok {
			t.Errorf("%s has sourcesContent with inlineSources=false: %s", name, raw)
		}
	}
}
