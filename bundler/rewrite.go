package bundler

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/tspath"
)

type edit struct {
	start int
	end   int
	text  string
}

// rewriteRequires parses emitted JavaScript and rewrites relative require()
// specifiers to absolute module ids. Non-relative specifiers are left in
// place so the runtime can hand them to Node.
func rewriteRequires(js, importer string, resolve func(importer, spec string) (string, bool, error)) (string, []string, error) {
	fileName := tspath.RootedFilePathFromNormalized("/emit.js")
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(fileName)),
	}, js, core.ScriptKindJS)
	if err := errorFromDiagnostics(importer, js, file.Diagnostics()); err != nil {
		return "", nil, fmt.Errorf("parse emitted javascript: %w", err)
	}

	var edits []edit
	var deps []string
	var resolveErr error
	if file.Statements != nil {
		for _, stmt := range file.Statements.Nodes {
			walkRequires(stmt, func(arg *ast.Node) {
				if resolveErr != nil {
					return
				}
				spec := arg.Text()
				resolved, external, err := resolve(importer, spec)
				if err != nil {
					resolveErr = err
					return
				}
				if external {
					return
				}
				deps = append(deps, resolved)
				edits = append(edits, edit{
					start: arg.Pos(),
					end:   arg.End(),
					text:  strconv.Quote(moduleID(resolved)),
				})
			})
		}
	}
	if resolveErr != nil {
		return "", nil, resolveErr
	}
	rewritten, err := applyEdits(js, edits)
	if err != nil {
		return "", nil, err
	}
	return rewritten, deps, nil
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
