package define

import (
	"strings"
	"testing"
)

func TestFoldIfFalseTrue(t *testing.T) {
	src := `
if (false) { dead(); }
if (true) { live(); }
if (false) { dead(); } else { kept(); }
if (true) { first(); } else { second(); }
false && removed();
true || alsoRemoved();
keep();
`
	out := ApplyDefineDrop(src, nil, nil)
	if strings.Contains(out, "dead") || strings.Contains(out, "removed") || strings.Contains(out, "alsoRemoved") || strings.Contains(out, "second") {
		t.Fatalf("fold left dead branches:\n%s", out)
	}
	for _, want := range []string{"live()", "kept()", "first()", "keep()"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
