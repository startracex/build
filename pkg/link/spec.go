package link

import "github.com/startracex/tsgo/pkg/ast"

func specifierText(expr *ast.Expression) string {
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return expr.Text()
	default:
		return ""
	}
}