package bundler

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/startracex/build/pkg/plugin"
	"github.com/startracex/build/pkg/sourcemap"
)

const defineMapSource = `export const mode = __MODE__;
console.log("drop-me");
export function label(): string {
  return "KEEP";
}
`

// TestDefineDropKeepsSourceMapAligned checks that define/drop leaves kept positions mapped to their original lines.
func TestDefineDropKeepsSourceMapAligned(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.ts"), defineMapSource)
	sourcemapFlag := true
	result, err := Build(context.Background(), Options{
		Mode:      ModeApp,
		Entries:   []string{filepath.Join(dir, "index.ts")},
		OutFile:   filepath.Join(dir, "out", "bundle.mjs"),
		SourceMap: &sourcemapFlag,
		Define:    map[string]string{"__MODE__": `"prod"`},
		Drop:      []string{"console"},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, mapData := outputsOf(t, result)
	if strings.Contains(code, "drop-me") {
		t.Fatalf("drop did not remove the call:\n%s", code)
	}
	if !strings.Contains(code, `"prod"`) {
		t.Fatalf("define did not replace the identifier:\n%s", code)
	}
	parsed := decodeTestSourceMap(t, mapData)
	assertMappedTo(t, code, parsed, `"prod"`, "__MODE__")
	assertMappedTo(t, code, parsed, `"KEEP"`, `return "KEEP";`)
	for _, segment := range parsed.segments {
		if segment.srcLine == 1 && segment.srcIdx == 0 {
			t.Fatalf("dropped line is still mapped: %+v", segment)
		}
	}
}

// TestDefineDropSplitBuild checks that rewritten modules still drive splitting and keep their source maps.
func TestDefineDropSplitBuild(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "src", "shared.ts"), `export const shared = "SHARED";`)
	writeFile(t, filepath.Join(dir, "src", "index.ts"), `import { shared } from "./shared";
export const mode = __MODE__ + shared;
console.log("drop-me");
`)
	sourcemapFlag := true
	result, err := Build(context.Background(), Options{
		Mode:      ModeApp,
		Entries:   []string{filepath.Join(dir, "src", "index.ts")},
		OutDir:    filepath.Join(dir, "dist"),
		SourceMap: &sourcemapFlag,
		Define:    map[string]string{"__MODE__": `"prod"`},
		Drop:      []string{"console"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var js, mapData string
	for _, file := range result.Files {
		switch {
		case strings.HasSuffix(file.Path, ".js"):
			js += file.Data
		case strings.HasSuffix(file.Path, ".js.map"):
			mapData += file.Data
		}
	}
	if strings.Contains(js, "drop-me") || strings.Contains(js, "__MODE__") {
		t.Fatalf("define/drop failed:\n%s", js)
	}
	if !strings.Contains(js, `"prod"`) || !strings.Contains(js, "SHARED") {
		t.Fatalf("output lost code:\n%s", js)
	}
	if !jsontext.Value(mapData).IsValid() || mapData == "" {
		t.Fatalf("split output has no usable source map:\n%s", mapData)
	}
}

func outputsOf(t *testing.T, result *Result) (code, mapData string) {
	t.Helper()
	for _, file := range result.Files {
		switch {
		case strings.HasSuffix(file.Path, ".map"):
			mapData = file.Data
		case strings.HasSuffix(file.Path, ".mjs"):
			code = file.Data
		}
	}
	if code == "" || mapData == "" {
		t.Fatalf("missing output: %#v", result.Files)
	}
	if !jsontext.Value(mapData).IsValid() {
		t.Fatalf("map is not JSON:\n%s", mapData)
	}
	return code, mapData
}

type testMapSegment struct {
	genLine, genCol int
	srcIdx          int
	srcLine, srcCol int
}

type testSourceMap struct {
	sourcesContent []string
	segments       []testMapSegment
}

func decodeTestSourceMap(t *testing.T, mapData string) testSourceMap {
	t.Helper()
	var raw struct {
		Sources        []string  `json:"sources"`
		SourcesContent []*string `json:"sourcesContent"`
		Mappings       string    `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(mapData), &raw); err != nil {
		t.Fatalf("map: %v", err)
	}
	out := testSourceMap{sourcesContent: make([]string, len(raw.Sources))}
	for i := range raw.Sources {
		if i < len(raw.SourcesContent) && raw.SourcesContent[i] != nil {
			out.sourcesContent[i] = *raw.SourcesContent[i]
		}
	}
	srcIdx, srcLine, srcCol := 0, 0, 0
	for genLine, line := range strings.Split(raw.Mappings, ";") {
		if line == "" {
			continue
		}
		genCol := 0
		for part := range strings.SplitSeq(line, ",") {
			values := decodeTestVLQ(part)
			if len(values) < 1 {
				continue
			}
			genCol += values[0]
			segment := testMapSegment{genLine: genLine, genCol: genCol, srcIdx: -1}
			if len(values) >= 4 {
				srcIdx += values[1]
				srcLine += values[2]
				srcCol += values[3]
				segment.srcIdx = srcIdx
				segment.srcLine = srcLine
				segment.srcCol = srcCol
			}
			out.segments = append(out.segments, segment)
		}
	}
	return out
}

// assertMappedTo checks that the closest mapping before generated points at a source line containing original.
func assertMappedTo(t *testing.T, code string, parsed testSourceMap, generated, original string) {
	t.Helper()
	offset := strings.Index(code, generated)
	if offset < 0 {
		t.Fatalf("%q missing from the bundle:\n%s", generated, code)
	}
	line := strings.Count(code[:offset], "\n")
	col := offset - (strings.LastIndexByte(code[:offset], '\n') + 1)
	var best *testMapSegment
	for i := range parsed.segments {
		segment := parsed.segments[i]
		if segment.genLine != line || segment.genCol > col {
			continue
		}
		if best == nil || segment.genCol > best.genCol {
			best = &parsed.segments[i]
		}
	}
	if best == nil || best.srcIdx < 0 {
		t.Fatalf("%q has no mapping on generated line %d:\n%s", generated, line, code)
	}
	if best.srcIdx >= len(parsed.sourcesContent) {
		t.Fatalf("segment %+v points at a missing source", *best)
	}
	lines := strings.Split(parsed.sourcesContent[best.srcIdx], "\n")
	if best.srcLine >= len(lines) {
		t.Fatalf("segment %+v is past the source", *best)
	}
	if !strings.Contains(lines[best.srcLine], original) {
		t.Fatalf("%q maps to %q, want a line with %q", generated, lines[best.srcLine], original)
	}
}

func decodeTestVLQ(s string) []int {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	index := make(map[byte]int, len(alphabet))
	for i := range len(alphabet) {
		index[alphabet[i]] = i
	}
	var out []int
	value, shift := 0, 0
	for i := 0; i < len(s); i++ {
		digit, ok := index[s[i]]
		if !ok {
			return out
		}
		value |= (digit & 31) << shift
		if digit&32 != 0 {
			shift += 5
			continue
		}
		if value&1 != 0 {
			out = append(out, -(value >> 1))
		} else {
			out = append(out, value>>1)
		}
		value, shift = 0, 0
	}
	return out
}

// TestTransformPluginMapsChain checks that transform maps compose across plugins instead of overwriting each other.
func TestTransformPluginMapsChain(t *testing.T) {
	const source = `export const a = "ORIGINAL";`
	maps := []string{
		`{"version":3,"sources":["input.js"],"names":[],"mappings":";AAAA"}`,
		`{"version":3,"sources":["input.js"],"names":[],"mappings":";;AACA"}`,
	}
	wants := []string{`"mappings":";AAAA"`, `"mappings":";;AAAA"`}
	for _, count := range []int{1, 2} {
		plugins := make([]plugin.Plugin, 0, count)
		for i := 0; i < count; i++ {
			idx := i
			plugins = append(plugins, plugin.Plugin{
				Transform: func(_ plugin.HookCtx, code, _ string) (string, *sourcemap.Map, error) {
					m, err := sourcemap.ParseMap(maps[idx])
					if err != nil {
						return "", nil, err
					}
					return "// p\n" + code, m, nil
				},
			})
		}
		code, m, err := plugin.RunTransform(plugins, plugin.HookCtx{}, source, source, "id")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(code, `"ORIGINAL"`) {
			t.Fatalf("transform output lost the source:\n%s", code)
		}
		if mapJSON := m.JSON(); !strings.Contains(mapJSON, wants[count-1]) {
			t.Fatalf("chained map = %q, want mappings %q", mapJSON, wants[count-1])
		}
	}
}
