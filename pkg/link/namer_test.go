package link

import "testing"

func TestNamerDollarSuffixCollision(t *testing.T) {
	n := &namer{used: map[string]int{}, taken: map[string]struct{}{}}
	if got := n.take("foo$2"); got != "foo$2" {
		t.Fatalf("first = %q", got)
	}
	if got := n.take("foo"); got != "foo" {
		t.Fatalf("second = %q", got)
	}
	if got := n.take("foo"); got != "foo$3" {
		t.Fatalf("third = %q (want foo$3 to skip taken foo$2)", got)
	}
}