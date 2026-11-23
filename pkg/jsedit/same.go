package jsedit

import "github.com/startracex/tsgo/pkg/ast"

// SameTokens reports whether two parses describe the same program. Positions,
// whitespace, comments, and optional semicolons are ignored, so a reprint of
// the same code compares equal while any transform the emitter applied does
// not: a downleveled operator, a rewritten class field, or a new helper
// statement all change a node kind or the statement list.
func SameTokens(aSrc string, a *ast.SourceFile, bSrc string, b *ast.SourceFile) bool {
	var aStmts, bStmts []*ast.Node
	if a != nil && a.Statements != nil {
		aStmts = a.Statements.Nodes
	}
	if b != nil && b.Statements != nil {
		bStmts = b.Statements.Nodes
	}
	if len(aStmts) != len(bStmts) {
		return false
	}
	for i := range aStmts {
		if !sameNode(aSrc, aStmts[i], bSrc, bStmts[i]) {
			return false
		}
	}
	return true
}

func sameNode(aSrc string, a *ast.Node, bSrc string, b *ast.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	var aKids, bKids []*ast.Node
	a.ForEachChild(func(child *ast.Node) bool {
		aKids = append(aKids, child)
		return false
	})
	b.ForEachChild(func(child *ast.Node) bool {
		bKids = append(bKids, child)
		return false
	})
	if len(aKids) != len(bKids) {
		return false
	}
	if len(aKids) == 0 {
		return tokenText(aSrc, a) == tokenText(bSrc, b)
	}
	for i := range aKids {
		if !sameNode(aSrc, aKids[i], bSrc, bKids[i]) {
			return false
		}
	}
	return true
}

// tokenText returns the text of n without leading trivia.
func tokenText(src string, n *ast.Node) string {
	start := TokenStart(src, n)
	end := n.End()
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return src[start:end]
}
