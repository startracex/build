package bundler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppBundleHandlesCommonJSDependency guards a CJS dependency whose sibling require used to loop forever in linking.
func TestAppBundleHandlesCommonJSDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.js"), `import core from "hl/lib/core";
export const v = core;
`)
	writeFile(t, filepath.Join(dir, "node_modules", "hl", "package.json"),
		`{"name":"hl","version":"1.0.0","exports":{"./lib/core":"./lib/core.js","./lib/*":"./lib/*.js"}}`)
	writeFile(t, filepath.Join(dir, "node_modules", "hl", "lib", "core.js"),
		`const lang = require("./languages/typescript");
module.exports = { lang };
`)
	writeFile(t, filepath.Join(dir, "node_modules", "hl", "lib", "languages", "typescript.js"),
		`module.exports = { name: "ts" };
`)
	out := filepath.Join(dir, "dist")
	result, err := Build(context.Background(), Options{
		Mode:    ModeApp,
		Entries: []string{filepath.Join(dir, "index.js")},
		OutDir:  out,
	})
	if err != nil {
		t.Fatal(err)
	}
	code := result.JavaScript()
	if !strings.Contains(code, "__cjsExports") {
		t.Fatalf("commonjs dependency was not bundled:\n%s", code)
	}
	// A synchronous require must not become a lazy chunk.
	for _, file := range result.Files {
		if strings.Contains(filepath.ToSlash(file.Path), "/chunks/") {
			t.Fatalf("require produced a chunk: %s\n%s", file.Path, code)
		}
	}
}
