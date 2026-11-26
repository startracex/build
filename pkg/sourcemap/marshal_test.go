package sourcemap

import (
	"strings"
	"testing"
)

// TestMarshalMapKeepsV1Encoding checks the v2 encoder matches v1: sorted keys, escaped HTML/JS, and U+FFFD for invalid UTF-8.
func TestMarshalMapKeepsV1Encoding(t *testing.T) {
	got, err := marshalMap(map[string]any{
		"b": "x<y>&z",
		"a": "line\u2028sep",
		"c": "\xff",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.HasPrefix(text, `{"a":`) {
		t.Fatalf("map keys are not sorted: %s", text)
	}
	for _, want := range []string{`\u003c`, `\u003e`, `\u0026`, `\u2028`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	// v2 writes the replacement rune directly where v1 wrote \ufffd; both decode to U+FFFD.
	if !strings.Contains(text, "\ufffd") {
		t.Errorf("invalid UTF-8 was not replaced: %s", text)
	}
}
