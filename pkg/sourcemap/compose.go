package sourcemap

import (
	"encoding/json/v2"
	"strings"
)

// ModuleMap is one module's transpile map plus original source text.
type ModuleMap struct {
	Display  string // path as written in "// file" linker comments
	Original string // original source content
	JSMap    string // transpile source map JSON (may be empty)
}

// ComposeLinked builds a source map for linked output by combining per-module
// transpile maps with "// path" section markers. Generated columns map to
// original columns when a transpile map is present; otherwise line identity.
func ComposeLinked(js, fileName string, modules []ModuleMap) string {
	byDisplay := map[string]ModuleMap{}
	for _, m := range modules {
		byDisplay[m.Display] = m
		// also allow slash-normalized lookup
		byDisplay[strings.ReplaceAll(m.Display, "\\", "/")] = m
	}

	decodedMaps := map[string]*decodedMap{}

	lines := strings.Split(js, "\n")
	var sources []string
	index := map[string]int{}
	contents := map[string]string{}
	var points []mapPointCol

	currentDisplay := ""
	sectionStart := 0
	srcLineInSection := 0

	flushIdentity := func(genLine int) {
		if currentDisplay == "" {
			return
		}
		m, ok := byDisplay[currentDisplay]
		srcName := currentDisplay
		srcContent := ""
		if ok {
			srcContent = m.Original
			if srcContent == "" {
				srcContent = readIfExists(currentDisplay)
			}
		} else {
			srcContent = readIfExists(currentDisplay)
		}
		si, seen := index[srcName]
		if !seen {
			si = len(sources)
			index[srcName] = si
			sources = append(sources, srcName)
			contents[srcName] = srcContent
		}
		points = append(points, mapPointCol{genLine: genLine, genCol: 0, source: si, srcLine: srcLineInSection, srcCol: 0})
	}

	for i, line := range lines {
		if name, ok := sourceComment(line); ok {
			currentDisplay = name
			sectionStart = i + 1
			srcLineInSection = 0
			if m, ok := byDisplay[name]; ok && strings.TrimSpace(m.JSMap) != "" {
				if decodedMaps[name] == nil {
					decodedMaps[name] = decodeMap(m.JSMap, m.Original)
				}
			}
			continue
		}
		if currentDisplay == "" {
			continue
		}
		if dm := decodedMaps[currentDisplay]; dm != nil {
			// Map this generated line relative to section using transpile map's gen lines.
			rel := max(i-sectionStart, 0)
			if segs := dm.segs[rel]; len(segs) > 0 {
				for _, s := range segs {
					srcName := currentDisplay
					if s.srcIdx >= 0 && s.srcIdx < len(dm.sources) {
						srcName = dm.sources[s.srcIdx]
					}
					srcContent := contents[srcName]
					if srcContent == "" {
						if s.srcIdx >= 0 && s.srcIdx < len(dm.content) {
							srcContent = dm.content[s.srcIdx]
						}
						if srcContent == "" {
							if m, ok := byDisplay[currentDisplay]; ok {
								srcContent = m.Original
							}
						}
					}
					si, seen := index[srcName]
					if !seen {
						si = len(sources)
						index[srcName] = si
						sources = append(sources, srcName)
						contents[srcName] = srcContent
					}
					points = append(points, mapPointCol{
						genLine: i, genCol: s.genCol, source: si, srcLine: s.srcLine, srcCol: s.srcCol,
					})
				}
				srcLineInSection++
				continue
			}
		}
		flushIdentity(i)
		if _, isComment := sourceComment(line); !isComment {
			srcLineInSection++
		}
	}

	content := make([]*string, len(sources))
	for i, name := range sources {
		text := contents[name]
		content[i] = &text
	}
	raw := map[string]any{
		"version":        3,
		"file":           fileName,
		"sources":        sources,
		"names":          []string{},
		"mappings":       encodeMappingsCol(points),
		"sourcesContent": content,
	}
	buf, err := marshalMap(raw)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

type decodedMap struct {
	sources []string
	content []string
	segs    map[int][]seg
}

type seg struct {
	genCol, srcIdx, srcLine, srcCol int
}

type mapPointCol struct {
	genLine, genCol, source, srcLine, srcCol int
}

func decodeMap(mapJSON, fallbackContent string) *decodedMap {
	var raw struct {
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if json.Unmarshal([]byte(mapJSON), &raw) != nil {
		return nil
	}
	d := &decodedMap{sources: raw.Sources, segs: map[int][]seg{}}
	d.content = make([]string, len(raw.Sources))
	for i := range raw.Sources {
		if i < len(raw.SourcesContent) {
			d.content[i] = raw.SourcesContent[i]
		}
		if d.content[i] == "" {
			d.content[i] = fallbackContent
		}
	}
	lines := strings.Split(raw.Mappings, ";")
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
			s := seg{genCol: genCol, srcIdx: -1}
			if n >= 4 {
				srcIdx += vals[1]
				srcLine += vals[2]
				srcCol += vals[3]
				s.srcIdx = srcIdx
				s.srcLine = srcLine
				s.srcCol = srcCol
			}
			d.segs[genLine] = append(d.segs[genLine], s)
		}
	}
	return d
}

func decodeVLQs(s string) ([]int, int) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var index [128]int
	for i := range index {
		index[i] = -1
	}
	for i, c := range chars {
		index[c] = i
	}
	var out []int
	i := 0
	for i < len(s) {
		var value, shift int
		for {
			if i >= len(s) {
				return out, len(out)
			}
			digit := -1
			if s[i] < 128 {
				digit = index[s[i]]
			}
			i++
			if digit < 0 {
				break
			}
			value |= (digit & 31) << shift
			shift += 5
			if digit&32 == 0 {
				break
			}
		}
		// Base64 VLQ stores the sign in the low bit: an odd value is negative.
		if value&1 != 0 {
			out = append(out, -(value >> 1))
		} else {
			out = append(out, value>>1)
		}
	}
	return out, len(out)
}

func encodeMappingsCol(points []mapPointCol) string {
	var b strings.Builder
	lastSrc, lastLine, lastCol, lastGenCol := 0, 0, 0, 0
	line := 0
	first := true
	for _, p := range points {
		for line < p.genLine {
			b.WriteByte(';')
			line++
			first = true
			lastGenCol = 0
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(vlq(p.genCol - lastGenCol))
		b.WriteString(vlq(p.source - lastSrc))
		b.WriteString(vlq(p.srcLine - lastLine))
		b.WriteString(vlq(p.srcCol - lastCol))
		lastGenCol = p.genCol
		lastSrc = p.source
		lastLine = p.srcLine
		lastCol = p.srcCol
	}
	return b.String()
}

// OffsetLines shifts every generated line in a source map by n (e.g. wrap preamble).
func OffsetLines(mapJSON string, n int) string {
	if n == 0 || strings.TrimSpace(mapJSON) == "" {
		return mapJSON
	}
	var raw map[string]any
	if json.Unmarshal([]byte(mapJSON), &raw) != nil {
		return mapJSON
	}
	mappings, _ := raw["mappings"].(string)
	raw["mappings"] = strings.Repeat(";", n) + mappings
	buf, err := marshalMap(raw)
	if err != nil {
		return mapJSON
	}
	return string(buf)
}

// DropLeadingLines removes the first n generated lines from a source map.
func DropLeadingLines(mapJSON string, n int) string {
	if n <= 0 || strings.TrimSpace(mapJSON) == "" {
		return mapJSON
	}
	var raw map[string]any
	if json.Unmarshal([]byte(mapJSON), &raw) != nil {
		return mapJSON
	}
	mappings, _ := raw["mappings"].(string)
	points := decodeAllPoints(mappings)
	var kept []mapPointCol
	for _, p := range points {
		if p.genLine < n {
			continue
		}
		p.genLine -= n
		kept = append(kept, p)
	}
	raw["mappings"] = encodeMappingsCol(kept)
	buf, err := marshalMap(raw)
	if err != nil {
		return mapJSON
	}
	return string(buf)
}
