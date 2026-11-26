package bundler

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tspath"
)

// TestRootDirFromCompilerOptions checks that library builds apply tsconfig rootDir without cmd/build.
func TestRootDirFromCompilerOptions(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "src")
	writeFile(t, filepath.Join(sourceDir, "index.ts"), `export const value = 1;
`)
	compiler := &core.CompilerOptions{}
	compiler.RootDir = tspath.RootedDirectoryPathFromAbsolute(filepath.ToSlash(sourceDir))

	result, err := Build(context.Background(), Options{
		Entries:         []string{filepath.Join(sourceDir, "index.ts")},
		Mode:            ModeLib,
		OutDir:          filepath.Join(dir, "dist"),
		CompilerOptions: compiler,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(dir, "dist", "index.js"))
	for _, file := range result.Files {
		if filepath.ToSlash(file.Path) == want {
			return
		}
	}
	t.Fatalf("rootDir from compiler options was ignored; got %v", result.Files)
}

// TestRootDirOptionWinsOverCompilerOptions checks that an explicit RootDir overrides tsconfig rootDir.
func TestRootDirOptionWinsOverCompilerOptions(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "src")
	writeFile(t, filepath.Join(sourceDir, "index.ts"), `export const value = 1;
`)
	compiler := &core.CompilerOptions{}
	compiler.RootDir = tspath.RootedDirectoryPathFromAbsolute(filepath.ToSlash(sourceDir))

	result, err := Build(context.Background(), Options{
		Entries:         []string{filepath.Join(sourceDir, "index.ts")},
		Mode:            ModeLib,
		OutDir:          filepath.Join(dir, "dist"),
		RootDir:         dir,
		CompilerOptions: compiler,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(dir, "dist", "src", "index.js"))
	for _, file := range result.Files {
		if filepath.ToSlash(file.Path) == want {
			return
		}
	}
	t.Fatalf("Options.RootDir did not win; got %v", result.Files)
}
