package define

import (
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

// fold collects one constant-folding pass; the caller repeats until no edit remains to catch branches that become constant only after an outer fold.
func fold(src string) []jsedit.Edit {
	file := jsedit.ParseEmittedJS(src)
	if file.Statements == nil {
		return nil
	}
	f := &folder{src: src}
	for _, stmt := range file.Statements.Nodes {
		f.visit(stmt)
	}
	return f.edits
}

type folder struct {
	src   string
	edits []jsedit.Edit
}

func (f *folder) visit(n *ast.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case ast.KindIfStatement:
		f.visitIf(n)
		return
	case ast.KindBinaryExpression:
		if f.visitLogical(n) {
			return
		}
	}
	n.ForEachChild(func(child *ast.Node) bool {
		f.visit(child)
		return false
	})
}

func (f *folder) visitIf(n *ast.Node) {
	ifStmt := n.AsIfStatement()
	value, ok := boolLiteral(ifStmt.Expression)
	if !ok {
		n.ForEachChild(func(child *ast.Node) bool {
			f.visit(child)
			return false
		})
		return
	}
	if value {
		f.keepBranch(n, ifStmt.ThenStatement, ifStmt.ElseStatement)
		return
	}
	if ifStmt.ElseStatement != nil {
		f.keepBranch(n, ifStmt.ElseStatement, ifStmt.ThenStatement)
		return
	}
	f.removeStatement(n)
}

// keepBranch keeps the selected branch; only the header is replaced so surviving statements keep their offsets and source map entries.
func (f *folder) keepBranch(n, keep, dead *ast.Node) {
	if isEmptyStatement(keep) {
		f.removeStatement(n)
		return
	}
	if keep.Kind != ast.KindBlock {
		f.dropBefore(n, keep)
		if dead != nil {
			f.dropAfter(keep, n)
		}
		f.visit(keep)
		return
	}
	block := keep.AsBlock()
	if block.Statements == nil || len(block.Statements.Nodes) == 0 {
		f.removeStatement(n)
		return
	}
	statements := block.Statements.Nodes
	if len(statements) == 1 {
		// Unwrap a single statement to avoid a bare block; fold it next pass since an inner edit would overlap the header range.
		first := statements[0]
		end := block.End()
		if dead != nil {
			end = n.End()
		}
		f.dropBefore(n, first)
		f.dropRange(first.End(), end)
		return
	}
	// Keep multi-statement branches as a plain block: braces preserve indentation and the branch's scope.
	f.dropBefore(n, keep)
	if dead != nil {
		f.dropAfter(keep, n)
	}
	for _, stmt := range statements {
		f.visit(stmt)
	}
}

// dropBefore removes the header text leading up to keep.
func (f *folder) dropBefore(n, keep *ast.Node) {
	f.dropRange(jsedit.TokenStart(f.src, n), jsedit.TokenStart(f.src, keep))
}

func (f *folder) dropAfter(keep, n *ast.Node) {
	f.dropRange(keep.End(), n.End())
}

func (f *folder) dropRange(start, end int) {
	if end <= start {
		return
	}
	f.edits = append(f.edits, jsedit.Edit{Start: start, End: end, Text: ""})
}

// visitLogical folds `false && x`/`true || x` to the constant and `true && x`/`false || x` to the right operand.
func (f *folder) visitLogical(n *ast.Node) bool {
	bin := n.AsBinaryExpression()
	if bin.OperatorToken == nil || bin.Left == nil || bin.Right == nil {
		return false
	}
	value, ok := boolLiteral(bin.Left)
	if !ok {
		return false
	}
	shortCircuits := false
	switch bin.OperatorToken.Kind {
	case ast.KindAmpersandAmpersandToken:
		shortCircuits = !value
	case ast.KindBarBarToken:
		shortCircuits = value
	default:
		return false
	}
	if shortCircuits {
		if stmt := expressionStatement(n); stmt != nil {
			f.removeStatement(stmt)
			return true
		}
		text := "false"
		if value {
			text = "true"
		}
		f.edits = append(f.edits, jsedit.Edit{
			Start: jsedit.TokenStart(f.src, n),
			End:   n.End(),
			Text:  text,
		})
		return true
	}
	if operandNeedsParens(bin.Right) {
		f.edits = append(f.edits, jsedit.Edit{
			Start: jsedit.TokenStart(f.src, n),
			End:   n.End(),
			Text:  "(" + nodeText(f.src, bin.Right) + ")",
		})
		return true
	}
	// Drop the left operand and operator but keep the right operand's offsets so its source map entries survive.
	f.edits = append(f.edits, jsedit.Edit{
		Start: jsedit.TokenStart(f.src, n),
		End:   jsedit.TokenStart(f.src, bin.Right),
		Text:  "",
	})
	f.visit(bin.Right)
	return true
}

func (f *folder) removeStatement(n *ast.Node) {
	start := n.Pos()
	tokenStart := jsedit.TokenStart(f.src, n)
	text := statementSeparator(f.src, n.End())
	if requiresStatementSlot(n) {
		start = tokenStart
		text = "{}"
	} else if start < 0 || start > tokenStart || leadingComment(f.src[start:tokenStart]) {
		start = tokenStart
	}
	f.edits = append(f.edits, jsedit.Edit{Start: start, End: n.End(), Text: text})
}

func boolLiteral(expr *ast.Node) (bool, bool) {
	expr = ast.SkipParentheses(expr)
	if expr == nil || !ast.IsBooleanLiteral(expr) {
		return false, false
	}
	return expr.Kind == ast.KindTrueKeyword, true
}

func isEmptyStatement(n *ast.Node) bool {
	if n == nil || n.Kind == ast.KindEmptyStatement {
		return true
	}
	if n.Kind != ast.KindBlock {
		return false
	}
	block := n.AsBlock()
	return block.Statements == nil || len(block.Statements.Nodes) == 0
}

func expressionStatement(n *ast.Node) *ast.Node {
	if n.Parent != nil && n.Parent.Kind == ast.KindExpressionStatement {
		return n.Parent
	}
	return nil
}

// operandNeedsParens reports whether n changes meaning as the operand of the surrounding operator.
func operandNeedsParens(n *ast.Node) bool {
	switch n.Kind {
	case ast.KindBinaryExpression, ast.KindConditionalExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindClassExpression,
		ast.KindYieldExpression, ast.KindAwaitExpression:
		return true
	}
	return false
}
