package define

import (
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

func foldConstantBranches(src string) string {
	file := jsedit.ParseEmittedJS(src)
	if file.Statements == nil {
		return src
	}
	var edits []jsedit.Edit
	for _, stmt := range file.Statements.Nodes {
		foldStmt(src, stmt, &edits)
	}
	if len(edits) == 0 {
		return src
	}
	out, err := jsedit.ApplyEdits(src, edits)
	if err != nil {
		return src
	}
	return out
}

func foldStmt(src string, stmt *ast.Node, edits *[]jsedit.Edit) {
	switch stmt.Kind {
	case ast.KindIfStatement:
		foldIf(src, stmt, edits)
	case ast.KindExpressionStatement:
		expr := stmt.AsExpressionStatement().Expression
		if expr == nil || expr.Kind != ast.KindBinaryExpression {
			return
		}
		bin := expr.AsBinaryExpression()
		if bin.OperatorToken == nil {
			return
		}
		switch bin.OperatorToken.Kind {
		case ast.KindAmpersandAmpersandToken:
			if v, ok := boolLiteralValue(bin.Left); ok && !v {
				*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
			}
		case ast.KindBarBarToken:
			if v, ok := boolLiteralValue(bin.Left); ok && v {
				*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
			}
		}
	}
}

func foldIf(src string, stmt *ast.Node, edits *[]jsedit.Edit) {
	ifStmt := stmt.AsIfStatement()
	cond := ifStmt.Expression
	val, ok := boolLiteralValue(cond)
	if !ok {
		return
	}
	if val {
		repl := stmtSource(src, ifStmt.ThenStatement)
		if repl == "" {
			*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
			return
		}
		*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: repl + "\n"})
		return
	}
	if ifStmt.ElseStatement != nil {
		repl := stmtSource(src, ifStmt.ElseStatement)
		if repl == "" {
			*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
			return
		}
		*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: repl + "\n"})
		return
	}
	*edits = append(*edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
}

func boolLiteralValue(expr *ast.Expression) (bool, bool) {
	if expr == nil || !ast.IsBooleanLiteral(expr) {
		return false, false
	}
	return expr.Kind == ast.KindTrueKeyword, true
}

func stmtSource(src string, stmt *ast.Statement) string {
	if stmt == nil {
		return ""
	}
	if stmt.Kind == ast.KindBlock {
		block := stmt.AsBlock()
		if block.Statements == nil || len(block.Statements.Nodes) == 0 {
			return ""
		}
		nodes := block.Statements.Nodes
		return src[nodes[0].Pos():nodes[len(nodes)-1].End()]
	}
	return src[stmt.Pos():stmt.End()]
}