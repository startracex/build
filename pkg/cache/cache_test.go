package cache

import "testing"

func TestTransformCacheKeyIgnoresOrder(t *testing.T) {
	a := TransformCacheKey(
		[]string{"beta", "alpha"},
		map[string]string{"X": "1", "Y": "2"},
		[]string{"debug", " test "},
	)
	b := TransformCacheKey(
		[]string{"alpha", "beta"},
		map[string]string{"Y": "2", "X": "1"},
		[]string{"TEST", "debug"},
	)
	if a != b {
		t.Fatalf("input order changed the key: %q != %q", a, b)
	}
}

// TestTransformCacheKeyDeterministic checks that the key does not depend on Go's
// randomized map iteration. encoding/json/v2 only sorts map keys when the
// deterministic option is requested, so this repeats the call to catch a
// regression.
func TestTransformCacheKeyDeterministic(t *testing.T) {
	define := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	first := TransformCacheKey([]string{"p", "q"}, define, []string{"debug"})
	for range 100 {
		if got := TransformCacheKey([]string{"q", "p"}, define, []string{"debug"}); got != first {
			t.Fatalf("same settings produced a different key: %q != %q", got, first)
		}
	}
}

func TestTransformCacheKeyTracksSettings(t *testing.T) {
	base := TransformCacheKey([]string{"p"}, map[string]string{"X": "1"}, nil)
	cases := map[string]string{
		"added plugin":  TransformCacheKey([]string{"p", "q"}, map[string]string{"X": "1"}, nil),
		"define value":  TransformCacheKey([]string{"p"}, map[string]string{"X": "2"}, nil),
		"define name":   TransformCacheKey([]string{"p"}, map[string]string{"Z": "1"}, nil),
		"dropped label": TransformCacheKey([]string{"p"}, map[string]string{"X": "1"}, []string{"debug"}),
	}
	for name, key := range cases {
		if key == base {
			t.Errorf("%s did not change the key", name)
		}
	}
}

func TestTransformCacheKeyDoesNotMutateInput(t *testing.T) {
	names := []string{"beta", "alpha"}
	drop := []string{"debug", "assert"}
	TransformCacheKey(names, map[string]string{"X": "1"}, drop)
	if names[0] != "beta" || drop[0] != "debug" {
		t.Fatalf("input order was changed: %v %v", names, drop)
	}
}

func TestKeySeparatesOptionFingerprints(t *testing.T) {
	c := &TranspileCache{}
	a := c.Key("path.ts", "src", "options-a", "transform")
	b := c.Key("path.ts", "src", "options-b", "transform")
	if a == b {
		t.Fatal("different option fingerprints produced the same key")
	}
}
