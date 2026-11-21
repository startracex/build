package bundler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
)

type edit struct {
	start int
	end   int
	text  string
}

func walkRequires(n *ast.Node, fn func(arg *ast.Node)) {
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
		walkRequires(child, fn)
		return false
	})
}

func walkImportCalls(n *ast.Node, fn func(arg *ast.Node)) {
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
		walkImportCalls(child, fn)
		return false
	})
}

func applyEdits(src string, edits []edit) (string, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b strings.Builder
	b.Grow(len(src) + len(edits)*16)
	last := 0
	for _, e := range edits {
		if e.start < last || e.end < e.start || e.end > len(src) {
			return "", fmt.Errorf("invalid require rewrite range %d:%d", e.start, e.end)
		}
		b.WriteString(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(src[last:])
	return b.String(), nil
}
