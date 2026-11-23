package modpath

import "testing"

func TestExtHelpers(t *testing.T) {
	cases := []struct {
		path     string
		ext      string
		lower    string
		stem     string
		baseStem string
	}{
		{"a/b/index.ts", ".ts", ".ts", "a/b/index", "index"},
		{"a/b/index.TSX", ".TSX", ".tsx", "a/b/index", "index"},
		{"a/b/index", "", "", "a/b/index", "index"},
		{"a/b/index.d.ts", ".ts", ".ts", "a/b/index.d", "index.d"},
	}
	for _, c := range cases {
		if got := Ext(c.path); got != c.ext {
			t.Errorf("Ext(%q) = %q want %q", c.path, got, c.ext)
		}
		if got := LowerExt(c.path); got != c.lower {
			t.Errorf("LowerExt(%q) = %q want %q", c.path, got, c.lower)
		}
		if got := Stem(c.path); got != c.stem {
			t.Errorf("Stem(%q) = %q want %q", c.path, got, c.stem)
		}
		if got := BaseStem(c.path); got != c.baseStem {
			t.Errorf("BaseStem(%q) = %q want %q", c.path, got, c.baseStem)
		}
	}
}

func TestReplaceExtHelper(t *testing.T) {
	if got := ReplaceExt("a/b/index.ts", ".js"); got != "a/b/index.js" {
		t.Errorf("ReplaceExt = %q", got)
	}
	if got := ReplaceExt("a/b/index.ts", ""); got != "a/b/index" {
		t.Errorf("ReplaceExt with empty ext = %q", got)
	}
}

func TestHasExtHelper(t *testing.T) {
	if !HasExt("a/b/index.d.ts", ".d.ts") {
		t.Error("HasExt should match .d.ts")
	}
	if HasExt("a/b/index.ts", ".d.ts") {
		t.Error("HasExt should not match .d.ts on index.ts")
	}
	if !HasExt("a/b/index.TS", ".ts") {
		t.Error("HasExt should ignore case")
	}
	if !HasAnyExt("a/b/index.tsx", ".js", ".jsx", ".tsx") {
		t.Error("HasAnyExt should match .tsx")
	}
	if HasAnyExt("a/b/index.css", ".js", ".jsx") {
		t.Error("HasAnyExt should not match .css")
	}
}

func TestSourceKindHelpers(t *testing.T) {
	if !IsTypeScriptSource("a/index.ts") || !IsTypeScriptSource("a/index.tsx") {
		t.Error("IsTypeScriptSource should accept .ts and .tsx")
	}
	if !IsTypeScriptSource("a/index.mts") || !IsTypeScriptSource("a/index.cts") {
		t.Error("IsTypeScriptSource should accept .mts and .cts")
	}
	if IsTypeScriptSource("a/index.js") {
		t.Error("IsTypeScriptSource should reject .js")
	}
	if !IsJavaScriptSource("a/index.jsx") || !IsJavaScriptSource("a/index.cjs") {
		t.Error("IsJavaScriptSource should accept .jsx and .cjs")
	}
	if IsJavaScriptSource("a/index.ts") {
		t.Error("IsJavaScriptSource should reject .ts")
	}
}

func TestDeclarationHelper(t *testing.T) {
	declarations := []string{"a/index.d.ts", "a/index.d.mts", "a/index.d.cts", "a/index.D.TS"}
	for _, path := range declarations {
		if !IsDeclaration(path) {
			t.Errorf("IsDeclaration(%q) should be true", path)
		}
	}
	sources := []string{"a/index.ts", "a/index.mts", "a/index.cts", "a/index.dtsx", "a/index.d.tsx"}
	for _, path := range sources {
		if IsDeclaration(path) {
			t.Errorf("IsDeclaration(%q) should be false", path)
		}
	}
}

func TestTypeScriptHelper(t *testing.T) {
	if !IsTypeScript("a/index.ts") || !IsTypeScript("a/index.mts") {
		t.Error("IsTypeScript should accept .ts and .mts")
	}
	if IsTypeScript("a/index.d.ts") || IsTypeScript("a/index.d.mts") || IsTypeScript("a/index.d.cts") {
		t.Error("IsTypeScript should reject declaration files")
	}
	if IsTypeScript("a/index.js") {
		t.Error("IsTypeScript should reject .js")
	}
}

func TestOutputKindHelpers(t *testing.T) {
	if !IsJavaScriptOutput("a/index.js") || !IsJavaScriptOutput("a/index.mjs") {
		t.Error("IsJavaScriptOutput should accept .js and .mjs")
	}
	if IsJavaScriptOutput("a/index.d.ts") || IsJavaScriptOutput("a/index.ts") {
		t.Error("IsJavaScriptOutput should reject TypeScript")
	}
}
