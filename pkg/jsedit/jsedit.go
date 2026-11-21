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
	Start int
	End   int
	Text  string
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
	if i := strings.IndexByte(js, '\n'); i >= 0 {
		return js[i+1:]
	}
	return ""
}

// StripSourceMappingComment removes trailing //# sourceMappingURL comments.
func StripSourceMappingComment(src string) string {
	lines := strings.Split(src, "\n")
	var kept []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//# sourceMappingURL=") || strings.HasPrefix(trim, "//@ sourceMappingURL=") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
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