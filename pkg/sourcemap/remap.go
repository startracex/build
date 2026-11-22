package sourcemap

import (
	"encoding/json"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
)

// RemapEdits rewrites generated positions in mapJSON after edits were applied
// to genSource (the file the map describes). Points that fall inside a deleted
// range are dropped; points after an edit shift by the net byte delta.
func RemapEdits(mapJSON, genSource string, edits []jsedit.Edit) string {
	if strings.TrimSpace(mapJSON) == "" || len(edits) == 0 {
		return mapJSON
	}
	var raw struct {
		Version        int       `json:"version"`
		File           string    `json:"file"`
		Sources        []string  `json:"sources"`
		SourcesContent []*string `json:"sourcesContent"`
		Names          []string  `json:"names"`
		Mappings       string    `json:"mappings"`
	}
	if json.Unmarshal([]byte(mapJSON), &raw) != nil || raw.Mappings == "" {
		return mapJSON
	}
	points := decodeAllPoints(raw.Mappings)
	if len(points) == 0 {
		return mapJSON
	}
	sorted := append([]jsedit.Edit(nil), edits...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Start < sorted[i].Start {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	newSrc, err := jsedit.ApplyEdits(genSource, sorted)
	if err != nil {
		return mapJSON
	}
	oldStarts := lineStartOffsets(genSource)
	newStarts := lineStartOffsets(newSrc)
	var kept []mapPointCol
	for _, p := range points {
		off := offsetAt(oldStarts, p.genLine, p.genCol)
		if off < 0 {
			continue
		}
		newOff, drop := mapOffsetThroughEdits(off, sorted)
		if drop {
			continue
		}
		line, col := lineColFromStarts(newStarts, newOff)
		kept = append(kept, mapPointCol{
			genLine: line, genCol: col,
			source: p.source, srcLine: p.srcLine, srcCol: p.srcCol,
		})
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
	buf, err := json.Marshal(out)
	if err != nil {
		return mapJSON
	}
	return string(buf)
}

// OffsetColumns shifts every generated column in a source map by n (wrap indent).
func OffsetColumns(mapJSON string, n int) string {
	if n == 0 || strings.TrimSpace(mapJSON) == "" {
		return mapJSON
	}
	var raw map[string]any
	if json.Unmarshal([]byte(mapJSON), &raw) != nil {
		return mapJSON
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
	buf, err := json.Marshal(raw)
	if err != nil {
		return mapJSON
	}
	return string(buf)
}

func mapOffsetThroughEdits(off int, sorted []jsedit.Edit) (newOff int, drop bool) {
	delta := 0
	for _, e := range sorted {
		if off >= e.End {
			delta += len(e.Text) - (e.End - e.Start)
			continue
		}
		if off >= e.Start {
			return 0, true
		}
		break
	}
	return off + delta, false
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
		for _, part := range strings.Split(line, ",") {
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
