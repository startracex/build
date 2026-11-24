package jsedit

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/scanner"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Edit is a byte-range replacement in a source string.
type Edit struct {
	Text  string
	End   int
	Start int
}

// ApplyEditsWithMap applies edits and returns the new source plus a net line delta.
func ApplyEditsWithMap(src string, edits []Edit) (string, int, error) {
	var delta int
	for _, e := range edits {
		old := 0
		if e.End > e.Start && e.End <= len(src) {
			old = strings.Count(src[e.Start:e.End], "\n")
		}
		delta += strings.Count(e.Text, "\n") - old
	}
	out, err := ApplyEdits(src, edits)
	if err != nil {
		return "", 0, err
	}
	return out, delta, nil
}

// ApplyEdits applies ordered non-overlapping edits to src.
func ApplyEdits(src string, edits []Edit) (string, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].Start < edits[j].Start })
	var b strings.Builder
	b.Grow(len(src) + len(edits)*16)
	last := 0
	for _, e := range edits {
		if e.Start < last || e.End < e.Start || e.End > len(src) {
			return "", fmt.Errorf("invalid require rewrite range %d:%d", e.Start, e.End)
		}
		b.WriteString(src[last:e.Start])
		b.WriteString(e.Text)
		last = e.End
	}
	b.WriteString(src[last:])
	return b.String(), nil
}

// StripShebang removes a leading #! line from JavaScript source.
func StripShebang(js string) string {
	if !strings.HasPrefix(js, "#!") {
		return js
	}
	if _, after, ok := strings.Cut(js, "\n"); ok {
		return after
	}
	return ""
}

// StripSourceMappingComment removes the sourceMappingURL comments from
// JavaScript source. Both the "//#" and the legacy "//@" spelling are
// removed, in the line-comment form and in the block-comment form.
//
// Line terminators follow the JavaScript rules: LF, CR, CRLF, LS (U+2028) and
// PS (U+2029). A comment only reaches its own line terminator, so code that
// follows the comment on the same logical line is kept.
func StripSourceMappingComment(src string) string {
	// Fast path: most modules carry no source map comment at all.
	if !strings.Contains(src, "sourceMappingURL=") {
		return src
	}
	exotic := strings.ContainsRune(src, '\u2028') || strings.ContainsRune(src, '\u2029')
	var b strings.Builder
	b.Grow(len(src))
	rest := src
	for len(rest) > 0 {
		lineEnd, next := cutLine(rest, exotic)
		if isSourceMappingLine(rest[:lineEnd]) {
			rest = rest[next:]
			continue
		}
		b.WriteString(rest[:next])
		rest = rest[next:]
	}
	return b.String()
}

// cutLine returns the end of the first line of src, excluding the line
// terminator, and the start of the next line.
func cutLine(src string, exotic bool) (lineEnd, next int) {
	lf := strings.IndexByte(src, '\n')
	limit := len(src)
	if lf >= 0 {
		limit = lf
	}
	// The terminator search stays inside the current line. Searching the whole
	// remainder instead turns a LF-only file into O(n^2).
	cr := strings.IndexByte(src[:limit], '\r')
	ls, ps := -1, -1
	if exotic {
		ls = strings.Index(src[:limit], "\u2028")
		ps = strings.Index(src[:limit], "\u2029")
	}
	best := -1
	switch {
	case cr >= 0:
		best = cr
	case ls >= 0 && (ps < 0 || ls < ps):
		best = ls
	case ps >= 0:
		best = ps
	}
	if best < 0 {
		if lf >= 0 {
			return lf, lf + 1
		}
		return len(src), len(src)
	}
	switch src[best] {
	case '\r':
		if best+1 < len(src) && src[best+1] == '\n' {
			return best, best + 2
		}
		return best, best + 1
	default: // U+2028 / U+2029
		return best, best + 3
	}
}

// isSourceMappingLine reports whether line is a sourceMappingURL comment.
func isSourceMappingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "//"):
		return hasSourceMappingURL(trimmed[2:])
	case strings.HasPrefix(trimmed, "/*"):
		return hasSourceMappingURL(strings.TrimSuffix(trimmed[2:], "*/"))
	}
	return false
}

// hasSourceMappingURL reports whether rest is a sourceMappingURL directive:
// "# sourceMappingURL=" or the legacy "@ sourceMappingURL=".
func hasSourceMappingURL(rest string) bool {
	if rest == "" || (rest[0] != '#' && rest[0] != '@') {
		return false
	}
	rest = rest[1:]
	if rest == "" || rest[0] != ' ' {
		return false
	}
	return strings.HasPrefix(rest[1:], "sourceMappingURL=")
}

// ParseEmittedJS parses transpiled JavaScript for AST-based rewrites.
func ParseEmittedJS(src string) *ast.SourceFile {
	fileName := tspath.RootedFilePathFromNormalized("/emit.js")
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(fileName)),
	}, src, core.ScriptKindJS)
}

// TokenStart returns the source index after leading trivia for n.
func TokenStart(src string, n *ast.Node) int {
	if n == nil || n.Pos() < 0 {
		return 0
	}
	return scanner.SkipTrivia(src, n.Pos())
}

// WalkRequires visits require(...) call arguments under n.
func WalkRequires(n *ast.Node, fn func(arg *ast.Node)) {
	if n == nil {
		return
	}
	if ast.IsRequireCall(n, true) {
		args := n.Arguments()
		if len(args) == 1 {
			fn(args[0])
		}
	}
	n.ForEachChild(func(child *ast.Node) bool {
		WalkRequires(child, fn)
		return false
	})
}

// WalkImportCalls visits import(...) call arguments under n.
func WalkImportCalls(n *ast.Node, fn func(arg *ast.Node)) {
	if n == nil {
		return
	}
	if ast.IsImportCall(n) {
		args := n.Arguments()
		if len(args) >= 1 && args[0] != nil {
			switch args[0].Kind {
			case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
				fn(args[0])
			}
		}
	}
	n.ForEachChild(func(child *ast.Node) bool {
		WalkImportCalls(child, fn)
		return false
	})
}

// IsIdentifierName reports whether name is a non-reserved JS identifier.
func IsIdentifierName(name string) bool {
	if name == "" || isReserved(name) {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '$' && r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '$' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isReserved(name string) bool {
	switch name {
	case "break", "case", "catch", "class", "const", "continue", "debugger", "default",
		"delete", "do", "else", "enum", "export", "extends", "false", "finally", "for",
		"function", "if", "import", "in", "instanceof", "new", "null", "return", "super",
		"switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with",
		"yield", "let", "static", "implements", "interface", "package", "private",
		"protected", "public", "await":
		return true
	default:
		return false
	}
}
