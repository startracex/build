package sourcemap

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// marshalMap encodes a map with the v1 encoder's settings: sorted keys, escaped HTML/JS chars, and invalid UTF-8 replaced with U+FFFD.
// Matching that escaping keeps output byte-stable since maps embed file contents.
func marshalMap(v any) ([]byte, error) {
	return json.Marshal(v,
		json.Deterministic(true),
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
		jsontext.AllowInvalidUTF8(true),
	)
}

// AppSourceMap builds a line-level map from linker source comments, reading marked sources through fs to fill sourcesContent.
// A nil fs reads the OS file system.
func AppSourceMap(fs vfs.FS, js, file string) *Map {
	lines := strings.Split(js, "\n")
	var sources []string
	index := map[string]int{}
	contents := map[string]string{}
	var points []mapPoint
	current := -1
	srcLine := 0
	for i, line := range lines {
		if name, ok := sourceComment(line); ok {
			n, seen := index[name]
			if !seen {
				n = len(sources)
				index[name] = n
				sources = append(sources, name)
				contents[name] = readIfExists(fs, name)
			}
			current = n
			srcLine = 0
		}
		if current < 0 {
			continue
		}
		points = append(points, mapPoint{genLine: i, source: current, srcLine: srcLine})
		if _, isComment := sourceComment(line); !isComment {
			srcLine++
		}
	}
	content := make([]*string, len(sources))
	for i, name := range sources {
		text := contents[name]
		content[i] = &text
	}
	raw := map[string]any{
		"version":        3,
		"file":           file,
		"sources":        sources,
		"names":          []string{},
		"mappings":       encodeMappings(points),
		"sourcesContent": content,
	}
	buf, err := marshalMap(raw)
	if err != nil {
		return newMap("{}")
	}
	return newMap(string(buf))
}

type mapPoint struct {
	genLine int
	source  int
	srcLine int
}

func encodeMappings(points []mapPoint) string {
	var b strings.Builder
	lastSrc := 0
	lastLine := 0
	lastCol := 0
	line := 0
	first := true
	for _, p := range points {
		for line < p.genLine {
			b.WriteByte(';')
			line++
			first = true
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(vlq(0))
		b.WriteString(vlq(p.source - lastSrc))
		b.WriteString(vlq(p.srcLine - lastLine))
		b.WriteString(vlq(0 - lastCol))
		lastSrc = p.source
		lastLine = p.srcLine
		lastCol = 0
	}
	return b.String()
}

// LinesBeforeSourceComment counts generated lines before the first linker // path marker.
func LinesBeforeSourceComment(js string) int {
	lines := strings.Split(js, "\n")
	for i, line := range lines {
		if _, ok := sourceComment(line); ok {
			return i
		}
	}
	return 0
}

func sourceComment(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	name, ok := strings.CutPrefix(trim, "// ")
	if !ok {
		return "", false
	}
	if asset.IsSource(name) {
		return filepath.ToSlash(name), true
	}
	return "", false
}

func readIfExists(fs vfs.FS, path string) string {
	buf, err := modpath.ReadFile(fs, filepath.FromSlash(path))
	if err != nil {
		return ""
	}
	return string(buf)
}

func vlq(n int) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	value := n
	if value < 0 {
		value = (-value << 1) | 1
	} else {
		value <<= 1
	}
	var out []byte
	for {
		digit := value & 31
		value >>= 5
		if value > 0 {
			digit |= 32
		}
		out = append(out, chars[digit])
		if value == 0 {
			break
		}
	}
	return string(out)
}

// sourceMapSource returns the path stored for source relative to outputDir (holding the emitted .map), matching tsc.
// A source under src with a map under build becomes "../src/index.ts", not "index.ts" relative to the source root.
func sourceMapSource(outputDir, source string) string {
	if outputDir == "" {
		return filepath.ToSlash(source)
	}
	rel, err := filepath.Rel(outputDir, source)
	if err != nil || rel == "." {
		return filepath.Base(source)
	}
	return filepath.ToSlash(rel)
}

// RetargetSourceMap rewrites file/sources for a per-module map.
// An empty outputDir keeps sourceName as given for callers that have not resolved the output location.
func RetargetSourceMap(m *Map, fileName, sourceName, sourceContent, outputDir, sourceAbs string) *Map {
	if m.IsZero() {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(m.JSON()), &raw) != nil {
		return m
	}
	name := sourceName
	if outputDir != "" && sourceAbs != "" {
		name = sourceMapSource(outputDir, sourceAbs)
	}
	raw["file"] = fileName
	raw["sourceRoot"] = ""
	raw["sources"] = []string{name}
	raw["sourcesContent"] = []string{sourceContent}
	buf, err := marshalMap(raw)
	if err != nil {
		return m
	}
	return newMap(string(buf))
}

// IdentitySourceMap maps every generated line to the same source line for unchanged output, such as a verbatim module.
// outputDir/sourceAbs, when set, place the source relative to the emitted map.
func IdentitySourceMap(file, sourceName, sourceContent, outputDir, sourceAbs string) *Map {
	lines := strings.Count(sourceContent, "\n") + 1
	points := make([]mapPointCol, 0, lines)
	for i := range lines {
		points = append(points, mapPointCol{genLine: i, source: 0, srcLine: i})
	}
	name := sourceName
	if outputDir != "" && sourceAbs != "" {
		name = sourceMapSource(outputDir, sourceAbs)
	}
	raw := map[string]any{
		"version":        3,
		"file":           file,
		"sourceRoot":     "",
		"sources":        []string{name},
		"names":          []string{},
		"mappings":       encodeMappingsCol(points),
		"sourcesContent": []string{sourceContent},
	}
	buf, err := marshalMap(raw)
	if err != nil {
		return newMap("{}")
	}
	return newMap(string(buf))
}

// RelativizeSourceMap rewrites sources relative to outputDir (holding the emitted .map), matching tsc.
// Relative input sources resolve against sourceBase first; absolute sources are used as-is.
// sourceRoot here is NOT the tsconfig field, which ApplySourceMapConfig applies.
func RelativizeSourceMap(m *Map, fileName, outputDir, sourceBase string) *Map {
	if m.IsZero() {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(m.JSON()), &raw) != nil {
		return m
	}
	raw["file"] = fileName
	sources, _ := raw["sources"].([]any)
	for i, source := range sources {
		name, _ := source.(string)
		if name == "" {
			continue
		}
		abs := filepath.FromSlash(name)
		if !filepath.IsAbs(abs) && sourceBase != "" {
			abs = filepath.Join(sourceBase, abs)
		}
		sources[i] = sourceMapSource(outputDir, abs)
	}
	raw["sources"] = sources
	buf, err := marshalMap(raw)
	if err != nil {
		return m
	}
	return newMap(string(buf))
}

// ApplySourceMapConfig finalizes a map with tsconfig options that change contents but not mapping positions.
// sourceRoot is written verbatim (tsc does not resolve it).
// inlineSources false removes sourcesContent to match tsc; true fills it from sourceContent when absent.
func ApplySourceMapConfig(m *Map, sourceRoot string, inlineSources bool, sourceContent string) *Map {
	if m.IsZero() {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(m.JSON()), &raw) != nil {
		return m
	}
	raw["sourceRoot"] = sourceRoot
	if inlineSources {
		if content, ok := raw["sourcesContent"].([]any); !ok || len(content) == 0 {
			raw["sourcesContent"] = []any{sourceContent}
		}
	} else {
		delete(raw, "sourcesContent")
	}
	buf, err := marshalMap(raw)
	if err != nil {
		return m
	}
	return newMap(string(buf))
}
