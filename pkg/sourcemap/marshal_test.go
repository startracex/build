package sourcemap

import (
	"strings"
	"testing"
)

// TestMarshalMapKeepsV1Encoding checks that the v2 encoder is configured to
// produce what the v1 encoder produced: sorted keys, escaped HTML and JS
// characters, and U+FFFD in place of invalid UTF-8.
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
	// The v2 encoder writes the replacement rune itself where v1 wrote the
	// \ufffd escape; both decode to U+FFFD.
	if !strings.Contains(text, "\ufffd") {
		t.Errorf("invalid UTF-8 was not replaced: %s", text)
	}
}
