package sourcemap

import (
	"encoding/json/v2"
	"testing"

	"github.com/startracex/build/pkg/jsedit"
)

func TestRemapEditsDeletionShiftsLines(t *testing.T) {
	src := "a();\nb();\nc();\n"
	mapText := testMap("out.js", "in.ts", "src", []mapPointCol{
		{genLine: 0, genCol: 0, source: 0, srcLine: 0, srcCol: 0},
		{genLine: 1, genCol: 0, source: 0, srcLine: 1, srcCol: 0},
		{genLine: 2, genCol: 0, source: 0, srcLine: 2, srcCol: 0},
	})
	out := RemapEdits(mapText, src, []jsedit.Edit{{Start: 5, End: 10, Text: ""}})
	points := decodeAllPoints(mustMappings(t, out))
	if len(points) != 2 {
		t.Fatalf("points = %+v", points)
	}
	want := []mapPointCol{
		{genLine: 0, genCol: 0, source: 0, srcLine: 0, srcCol: 0},
		{genLine: 1, genCol: 0, source: 0, srcLine: 2, srcCol: 0},
	}
	for i, p := range points {
		if p != want[i] {
			t.Fatalf("point %d = %+v, want %+v", i, p, want[i])
		}
	}
}

func TestRemapEditsReplacementKeepsPoint(t *testing.T) {
	src := "const x = __DEV__;\n"
	mapText := testMap("out.js", "in.ts", "src", []mapPointCol{
		{genLine: 0, genCol: 10, source: 0, srcLine: 0, srcCol: 10},
		{genLine: 0, genCol: 17, source: 0, srcLine: 0, srcCol: 17},
	})
	out := RemapEdits(mapText, src, []jsedit.Edit{{Start: 10, End: 17, Text: "false"}})
	points := decodeAllPoints(mustMappings(t, out))
	if len(points) != 2 {
		t.Fatalf("points = %+v", points)
	}
	if points[0].genCol != 10 || points[0].srcCol != 10 {
		t.Fatalf("replaced point was moved: %+v", points[0])
	}
	if points[1].genCol != 15 || points[1].srcCol != 17 {
		t.Fatalf("trailing point was not shifted: %+v", points[1])
	}
}

func TestRemapEditsDropsDeletedPoints(t *testing.T) {
	src := "dead();\nlive();\n"
	mapText := testMap("out.js", "in.ts", "src", []mapPointCol{
		{genLine: 0, genCol: 0, source: 0, srcLine: 0, srcCol: 0},
		{genLine: 1, genCol: 0, source: 0, srcLine: 1, srcCol: 0},
	})
	out := RemapEdits(mapText, src, []jsedit.Edit{{Start: 0, End: 8, Text: ""}})
	points := decodeAllPoints(mustMappings(t, out))
	if len(points) != 1 || points[0].srcLine != 1 {
		t.Fatalf("points = %+v", points)
	}
	if points[0].genLine != 0 {
		t.Fatalf("kept point was not shifted: %+v", points[0])
	}
}

func TestRemapEditsStagesChain(t *testing.T) {
	src := "if (__DEV__) {\nlive();\n}\n"
	first := []jsedit.Edit{{Start: 4, End: 11, Text: "true"}}
	second := []jsedit.Edit{{Start: 0, End: 12, Text: ""}, {Start: 21, End: 22, Text: ""}}
	step1, err := jsedit.ApplyEdits(src, first)
	if err != nil {
		t.Fatal(err)
	}
	mapText := testMap("out.js", "in.ts", "src", []mapPointCol{
		{genLine: 1, genCol: 0, source: 0, srcLine: 1, srcCol: 0},
	})
	for _, stage := range []struct {
		src   string
		edits []jsedit.Edit
	}{{src, first}, {step1, second}} {
		mapText = RemapEdits(mapText, stage.src, stage.edits)
	}
	points := decodeAllPoints(mustMappings(t, mapText))
	if len(points) != 1 {
		t.Fatalf("points = %+v", points)
	}
	if points[0].genLine != 0 || points[0].srcLine != 1 {
		t.Fatalf("point = %+v", points[0])
	}
}

func TestRemapEditsNoEdits(t *testing.T) {
	mapText := testMap("out.js", "in.ts", "src", []mapPointCol{
		{genLine: 0, genCol: 0, source: 0, srcLine: 0, srcCol: 0},
	})
	if out := RemapEdits(mapText, "a();", nil); out != mapText {
		t.Fatalf("empty edit list changed the map: %s", out)
	}
}

// TestVLQRoundTrip guards the sign bit of the base64 VLQ decoder: every
// negative delta must decode back to the value that produced it.
func TestVLQRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, -1, 2, -2, 31, -31, 1024, -1024} {
		values, count := decodeVLQs(vlq(n))
		if count != 1 || values[0] != n {
			t.Fatalf("vlq(%d) = %q decodes to %v", n, vlq(n), values)
		}
	}
	points := []mapPointCol{
		{genLine: 0, genCol: 12, source: 0, srcLine: 7, srcCol: 3},
		{genLine: 0, genCol: 40, source: 0, srcLine: 5, srcCol: 0},
		{genLine: 3, genCol: 0, source: 0, srcLine: 12, srcCol: 18},
	}
	decoded := decodeAllPoints(encodeMappingsCol(points))
	if len(decoded) != len(points) {
		t.Fatalf("decoded %d points, want %d", len(decoded), len(points))
	}
	for i, p := range decoded {
		if p != points[i] {
			t.Fatalf("point %d = %+v, want %+v", i, p, points[i])
		}
	}
}

func testMap(file, source, content string, points []mapPointCol) string {
	raw := map[string]any{
		"version":        3,
		"file":           file,
		"sources":        []string{source},
		"names":          []string{},
		"mappings":       encodeMappingsCol(points),
		"sourcesContent": []string{content},
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return string(buf)
}

func mustMappings(t *testing.T, mapText string) string {
	t.Helper()
	var raw struct {
		Mappings string `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(mapText), &raw); err != nil {
		t.Fatalf("map is not JSON: %v\n%s", err, mapText)
	}
	return raw.Mappings
}
