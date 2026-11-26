package jsedit

import "testing"

func TestStripSourceMappingComment(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"none", "const a = 1;\n", "const a = 1;\n"},
		{"lf", "a\n//# sourceMappingURL=x.map\nb\n", "a\nb\n"},
		{"crlf", "a\r\n//# sourceMappingURL=x.map\r\nb\r\n", "a\r\nb\r\n"},
		{"cr", "a\r//# sourceMappingURL=x.map\rb\r", "a\rb\r"},
		{"ls", "a\u2028//# sourceMappingURL=x.map\u2028b\u2028", "a\u2028b\u2028"},
		{"ps", "a\u2029//# sourceMappingURL=x.map\u2029b\u2029", "a\u2029b\u2029"},
		{"legacy-at", "a\n//@ sourceMappingURL=x.map\nb\n", "a\nb\n"},
		{"indented", "a\n   //# sourceMappingURL=x.map\nb\n", "a\nb\n"},
		{"block", "a\n/*# sourceMappingURL=x.map */\nb\n", "a\nb\n"},
		{"block-at", "a\n/*@ sourceMappingURL=x.map */\nb\n", "a\nb\n"},
		{"block-indented", "a\n  /*# sourceMappingURL=x.map */  \nb\n", "a\nb\n"},
		{"mixed", "a\r\nb\nc\r//# sourceMappingURL=x.map\nd", "a\r\nb\nc\rd"},
		{"code-after-ls", "//# sourceMappingURL=x.map\u2028console.log(1)", "console.log(1)"},
		{"code-after-cr", "//# sourceMappingURL=x.map\rconsole.log(1)", "console.log(1)"},
		{"trailing", "a\n//# sourceMappingURL=x.map", "a\n"},
		{"mid-file", "//# sourceMappingURL=x.map\ncode();\n", "code();\n"},
		{"not-a-directive", "a\n// sourceMappingURL=x.map\nb\n", "a\n// sourceMappingURL=x.map\nb\n"},
		{"no-space", "a\n//#sourceMappingURL=x.map\nb\n", "a\n//#sourceMappingURL=x.map\nb\n"},
		{"inline", "a\nconst u = \"//# sourceMappingURL=x.map\";\nb\n", "a\nconst u = \"//# sourceMappingURL=x.map\";\nb\n"},
	}
	for _, c := range cases {
		if got := StripSourceMappingComment(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestStripSourceMappingCommentNoAlloc guards the fast path: a module without a map comment must not be copied.
func TestStripSourceMappingCommentNoAlloc(t *testing.T) {
	src := "const a = 1;\nconst b = 2;\n"
	if allocs := testing.AllocsPerRun(100, func() { _ = StripSourceMappingComment(src) }); allocs != 0 {
		t.Errorf("expected no allocation for source without a map comment, got %v", allocs)
	}
}
