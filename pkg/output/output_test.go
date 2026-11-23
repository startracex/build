package output

import (
	"regexp"
	"testing"
)

func TestRenderEntryFileName(t *testing.T) {
	got := RenderEntryFileName("dist/[name].[format].js", "index", FormatCJS, "")
	want := "dist/index.cjs.js"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderEntryFileNameHash(t *testing.T) {
	hash := "0123456789abcdef"
	got := RenderEntryFileName("dist/[name].[hash:6].js", "index", FormatESM, hash)
	want := "dist/index.012345.js"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = RenderEntryFileName("dist/[name].[hash].js", "index", FormatESM, hash)
	if !regexp.MustCompile(`^dist/index\.[0-9a-f]{8}\.js$`).MatchString(got) {
		t.Fatalf("default hash length: got %q", got)
	}
}

func TestContentHashStable(t *testing.T) {
	first := ContentHash("hello")
	second := ContentHash("hello")
	if first != second || len(first) != 64 {
		t.Fatalf("hash = %q, %q", first, second)
	}
}
