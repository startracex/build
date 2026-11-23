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
	if IsTypeScriptSource("a/index.js") {
		t.Error("IsTypeScriptSource should reject .js")
	}
	if !IsJavaScriptSource("a/index.jsx") || !IsJavaScriptSource("a/index.cjs") {
		t.Error("IsJavaScriptSource should accept .jsx and .cjs")
	}
	if !IsSourcePath("a/index.mts") || !IsSourcePath("a/index.json") {
		t.Error("IsSourcePath should accept .mts and .json")
	}
	if IsSourcePath("a/index.css") {
		t.Error("IsSourcePath should reject .css")
	}
	if !IsDeclarationPath("a/index.d.ts") || IsDeclarationPath("a/index.ts") {
		t.Error("IsDeclarationPath is wrong")
	}
	if !IsJSONPath("a/index.json") || IsJSONPath("a/index.ts") {
		t.Error("IsJSONPath is wrong")
	}
	if !IsHTMLPath("a/index.html") || IsHTMLPath("a/index.htm") {
		t.Error("IsHTMLPath is wrong")
	}
}

func TestOutputKindHelpers(t *testing.T) {
	if got := JavascriptOutputAbs("a/index.ts"); got != "a/index.js" {
		t.Errorf("JavascriptOutputAbs = %q", got)
	}
	if got := JavascriptOutputAbs("a/index.json"); got != "a/index.js" {
		t.Errorf("JavascriptOutputAbs for json = %q", got)
	}
	if got := JavascriptOutputAbs("a/index.css"); got != "a/index.css" {
		t.Errorf("JavascriptOutputAbs for css = %q", got)
	}
	if !IsJavaScriptOutput("a/index.js") || !IsJavaScriptOutput("a/index.mjs") {
		t.Error("IsJavaScriptOutput should accept .js and .mjs")
	}
	if IsJavaScriptOutput("a/index.d.ts") {
		t.Error("IsJavaScriptOutput should reject .d.ts")
	}
	if !IsTypeScriptPath("a/index.ts") || IsTypeScriptPath("a/index.d.ts") {
		t.Error("IsTypeScriptPath is wrong")
	}
}