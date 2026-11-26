package output

import (
	"regexp"
	"testing"
)

func TestRenderEntryFileName(t *testing.T) {
	got := RenderEntryFileName("dist/[name].[format].js", "index", "index", FormatCJS, "")
	want := "dist/index.cjs.js"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderEntryFileNameHash(t *testing.T) {
	hash := "0123456789abcdef"
	got := RenderEntryFileName("dist/[name].[hash:6].js", "index", "index", FormatESM, hash)
	want := "dist/index.012345.js"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = RenderEntryFileName("dist/[name].[hash].js", "index", "index", FormatESM, hash)
	if !regexp.MustCompile(`^dist/index\.[0-9a-f]{8}\.js$`).MatchString(got) {
		t.Fatalf("default hash length: got %q", got)
	}
}

func TestRenderEntryFileNamePath(t *testing.T) {
	got := RenderEntryFileName("dist/[path].js", "index", "a/index", FormatESM, "")
	want := "dist/a/index.js"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHasHashPlaceholder(t *testing.T) {
	cases := map[string]bool{
		"dist/[hash].js":   true,
		"dist/[hash:8].js": true,
		"dist/[hash":       false,
		"dist/[hash:]":     false,
		"dist/[hash:8":     false,
		"dist/hash].js":    false,
	}
	for pattern, want := range cases {
		if got := HasHashPlaceholder(pattern); got != want {
			t.Errorf("HasHashPlaceholder(%q) = %v, want %v", pattern, got, want)
		}
	}
}

func TestContentHashStable(t *testing.T) {
	first := ContentHash("hello")
	second := ContentHash("hello")
	if first != second || len(first) != 64 {
		t.Fatalf("hash = %q, %q", first, second)
	}
}

// TestRenderFileNamePlaceholders checks every placeholder; [ext] expands without the dot, so ".[ext]" yields ".js".
func TestRenderFileNamePlaceholders(t *testing.T) {
	cases := []struct {
		pattern string
		parts   FileNameParts
		want    string
	}{
		{"[name]-[hash:4].[ext]", FileNameParts{Name: "logo", Ext: ".png", Hash: "abcdef0123"}, "logo-abcd.png"},
		{"[path].[ext]", FileNameParts{Path: "img/logo", Ext: ".png"}, "img/logo.png"},
		{"[name].[format].js", FileNameParts{Name: "index", Format: "esm"}, "index.esm.js"},
		{"chunks/[name]-[hash].js", FileNameParts{Name: "shared", Hash: "0123456789"}, "chunks/shared-01234567.js"},
		{"[type]/[name].[ext]", FileNameParts{Type: "entry", Name: "index", Ext: ".js"}, "entry/index.js"},
		{"[type]/[name]-[hash:4].[ext]", FileNameParts{Type: "chunk", Name: "lazy", Ext: ".js", Hash: "abcdef01"}, "chunk/lazy-abcd.js"},
		// A missing part expands to nothing so optional segments can be dropped.
		{"assets/[name][ext]", FileNameParts{Name: "raw", Ext: ""}, "assets/raw"},
	}
	for _, tc := range cases {
		if got := RenderFileName(tc.pattern, tc.parts); got != tc.want {
			t.Errorf("RenderFileName(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

// TestRenderEntryFileNameKeepsOldShape checks the compatibility wrapper still expands its original placeholders.
func TestRenderEntryFileNameKeepsOldShape(t *testing.T) {
	got := RenderEntryFileName("[name]-[hash:3].[format].js", "index", "sub/index", "esm", "123456789")
	if got != "index-123.esm.js" {
		t.Fatalf("got %q", got)
	}
}
