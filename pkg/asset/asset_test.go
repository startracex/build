package asset

import (
	"testing"

	"github.com/startracex/build/pkg/modpath"
)

func TestIsSource(t *testing.T) {
	accepted := []string{"a/index.ts", "a/index.tsx", "a/index.mts", "a/index.cts", "a/index.js", "a/index.jsx", "a/index.mjs", "a/index.cjs", "a/index.json"}
	for _, path := range accepted {
		if !IsSource(path) {
			t.Errorf("IsSource(%q) should be true", path)
		}
	}
	rejected := []string{"a/index.css", "a/index.html", "a/index.txt", "a/index"}
	for _, path := range rejected {
		if IsSource(path) {
			t.Errorf("IsSource(%q) should be false", path)
		}
	}
}

func TestSupportedSource(t *testing.T) {
	if !SupportedSource("a/index.ts") {
		t.Error("SupportedSource should accept .ts")
	}
	if !SupportedSource("a/style.css") {
		t.Error("SupportedSource should accept .css")
	}
	if SupportedSource("a/archive.xyz") {
		t.Error("SupportedSource should reject an unknown extension")
	}
}

func TestJavascriptOutputAbs(t *testing.T) {
	if got := modpath.JavascriptOutputAbs("a/index.ts"); got != "a/index.js" {
		t.Errorf("JavascriptOutputAbs = %q", got)
	}
	if got := modpath.JavascriptOutputAbs("a/index.css"); got != "a/index.css" {
		t.Errorf("JavascriptOutputAbs for css = %q", got)
	}
}
