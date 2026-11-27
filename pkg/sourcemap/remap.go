package sourcemap

import (
	"encoding/json/v2"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
)

// RemapEdits rewrites generated positions in m after edits to genSource, the file the map describes.
// Points in a deleted range are dropped, points in a replaced range move to the replacement, and later points shift by the net byte delta.
func RemapEdits(m *Map, genSource string, edits []jsedit.Edit) *Map {
	if m.IsZero() || len(edits) == 0 {
		return m
	}
	var raw struct {
		Version        int       `json:"version"`
		File           string    `json:"file"`
		Sources        []string  `json:"sources"`
		SourcesContent []*string `json:"sourcesContent"`
		Names          []string  `json:"names"`
		Mappings       string    `json:"mappings"`
	}
	if json.Unmarshal([]byte(m.JSON()), &raw) != nil || raw.Mappings == "" {
		return m
	}
	points := decodeAllPoints(raw.Mappings)
	if len(points) == 0 {
		return m
	}
	sorted := append([]jsedit.Edit(nil), edits...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Start < sorted[i].Start {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	newSrc, err := jsedit.ApplyEdits(genSource, sorted)
	if err != nil {
		return m
	}
	oldStarts := lineStartOffsets(genSource)
	newStarts := lineStartOffsets(newSrc)
	var kept []mapPointCol
	for _, p := range points {
		off := offsetAt(oldStarts, p.genLine, p.genCol)
		if off < 0 {
			continue
		}
		newOff, drop := mapOffsetThroughEdits(genSource, off, sorted)
		if drop {
			continue
		}
		line, col := lineColFromStarts(newStarts, newOff)
		point := mapPointCol{
			genLine: line, genCol: col,
			source: p.source, srcLine: p.srcLine, srcCol: p.srcCol,
		}
		// Clamping a replaced region can put several points on one position; keeping the first preserves sorted mappings.
		if n := len(kept); n > 0 && kept[n-1].genLine == line && kept[n-1].genCol == col {
			continue
		}
		kept = append(kept, point)
	}
	out := map[string]any{
		"version":  3,
		"file":     raw.File,
		"sources":  raw.Sources,
		"names":    raw.Names,
		"mappings": encodeMappingsCol(kept),
	}
	if raw.SourcesContent != nil {
		out["sourcesContent"] = raw.SourcesContent
	}
	buf, err := marshalMap(out)
	if err != nil {
		return m
	}
	return newMap(string(buf))
}

// OffsetColumns shifts every generated column in m by n (wrap indent).
func OffsetColumns(m *Map, n int) *Map {
	if n == 0 || m.IsZero() {
		return m
	}
	var raw map[string]any
	if json.Unmarshal([]byte(m.JSON()), &raw) != nil {
		return m
	}
	mappings, _ := raw["mappings"].(string)
	points := decodeAllPoints(mappings)
	for i := range points {
		points[i].genCol += n
		if points[i].genCol < 0 {
			points[i].genCol = 0
		}
	}
	raw["mappings"] = encodeMappingsCol(points)
	buf, err := marshalMap(raw)
	if err != nil {
		return m
	}
	return newMap(string(buf))
}

func mapOffsetThroughEdits(src string, off int, sorted []jsedit.Edit) (newOff int, drop bool) {
	delta := 0
	for _, e := range sorted {
		if off >= e.End {
			if off == e.End && deletedTrailer(src, e) {
				// Such a point would land at the previous line's end and claim removed code.
				return 0, true
			}
			delta += len(e.Text) - (e.End - e.Start)
			continue
		}
		if off >= e.Start {
			// A deleted range has no code to point at; a replaced range keeps its mapping at the edit's position.
			if e.Text == "" {
				return 0, true
			}
			return e.Start + delta, false
		}
		break
	}
	return off + delta, false
}

// deletedTrailer reports whether e removes a whole statement, so a mapping at e.End belongs to the removed code.
func deletedTrailer(src string, e jsedit.Edit) bool {
	if e.Text != "" || e.End <= e.Start || e.End > len(src) {
		return false
	}
	switch src[e.End-1] {
	case ';', '}':
	default:
		return false
	}
	for i := e.End; i < len(src); i++ {
		switch src[i] {
		case ' ', '\t', '\r':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func decodeAllPoints(mappings string) []mapPointCol {
	var points []mapPointCol
	lines := strings.Split(mappings, ";")
	srcIdx, srcLine, srcCol := 0, 0, 0
	for genLine, line := range lines {
		if line == "" {
			continue
		}
		genCol := 0
		for part := range strings.SplitSeq(line, ",") {
			vals, n := decodeVLQs(part)
			if n < 1 {
				continue
			}
			genCol += vals[0]
			p := mapPointCol{genLine: genLine, genCol: genCol, source: -1}
			if n >= 4 {
				srcIdx += vals[1]
				srcLine += vals[2]
				srcCol += vals[3]
				p.source = srcIdx
				p.srcLine = srcLine
				p.srcCol = srcCol
			}
			points = append(points, p)
		}
	}
	return points
}

func lineStartOffsets(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func offsetAt(lineStarts []int, line, col int) int {
	if line < 0 || line >= len(lineStarts) {
		return -1
	}
	return lineStarts[line] + col
}

func lineColFromStarts(starts []int, off int) (line, col int) {
	if off < 0 {
		return 0, 0
	}
	// last start <= off
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, off - starts[lo]
}
