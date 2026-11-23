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

// marshalMap encodes a source map object with the settings the v1 encoder used:
// sorted keys, '<', '>', '&', U+2028 and U+2029 escaped, and invalid UTF-8
// replaced with U+FFFD. Source maps embed file contents, so keeping the same
// escaping leaves the output byte-stable for the same input.
func marshalMap(v any) ([]byte, error) {
	return json.Marshal(v,
		json.Deterministic(true),
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
		jsontext.AllowInvalidUTF8(true),
	)
}

// AppSourceMap builds a line-level source map from linker source comments. It
// reads the marked sources through fs to fill sourcesContent; a nil fs reads the
// OS file system.
func AppSourceMap(fs vfs.FS, js, file string) string {
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
		return "{}"
	}
	return string(buf)
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

// RetargetSourceMap rewrites file/sources for a per-module map.
func RetargetSourceMap(mapText, fileName, sourceName, sourceContent string) string {
	if strings.TrimSpace(mapText) == "" {
		return ""
	}
	var raw map[string]any
	if json.Unmarshal([]byte(mapText), &raw) != nil {
		return mapText
	}
	raw["file"] = fileName
	raw["sources"] = []string{sourceName}
	raw["sourcesContent"] = []string{sourceContent}
	buf, err := marshalMap(raw)
	if err != nil {
		return mapText
	}
	return string(buf)
}

// IdentitySourceMap maps every generated line to the same source line. It
// describes output whose text is unchanged, such as a module kept verbatim.
func IdentitySourceMap(file, sourceName, sourceContent string) string {
	lines := strings.Count(sourceContent, "\n") + 1
	points := make([]mapPointCol, 0, lines)
	for i := range lines {
		points = append(points, mapPointCol{genLine: i, source: 0, srcLine: i})
	}
	raw := map[string]any{
		"version":        3,
		"file":           file,
		"sources":        []string{sourceName},
		"names":          []string{},
		"mappings":       encodeMappingsCol(points),
		"sourcesContent": []string{sourceContent},
	}
	buf, err := marshalMap(raw)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

// RelativizeDeclarationMap makes absolute source paths relative to root.
func RelativizeDeclarationMap(mapText, fileName, root string) string {
	if strings.TrimSpace(mapText) == "" {
		return ""
	}
	var raw map[string]any
	if json.Unmarshal([]byte(mapText), &raw) != nil {
		return mapText
	}
	raw["file"] = fileName
	sources, _ := raw["sources"].([]any)
	for i, source := range sources {
		name, _ := source.(string)
		cleaned := filepath.FromSlash(name)
		if filepath.IsAbs(cleaned) {
			sources[i] = modpath.DisplayRel(root, cleaned)
		}
	}
	raw["sources"] = sources
	buf, err := marshalMap(raw)
	if err != nil {
		return mapText
	}
	return string(buf)
}
