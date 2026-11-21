package treeshake

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/scanner"
)

func IsPureDecl(src string, stmt *ast.Node) bool {
	switch stmt.Kind {
	case ast.KindFunctionDeclaration:
		return true
	case ast.KindClassDeclaration:
		return isPureClass(src, stmt)
	case ast.KindVariableStatement:
		listNode := stmt.AsVariableStatement().DeclarationList
		if listNode == nil {
			return true
		}
		list := listNode.AsVariableDeclarationList()
		if list.Declarations == nil {
			return true
		}
		for _, declNode := range list.Declarations.Nodes {
			init := declNode.AsVariableDeclaration().Initializer
			if init != nil && !IsPureExpr(src, init) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isPureClass(src string, stmt *ast.Node) bool {
	cls := stmt.AsClassDeclaration()
	if cls.HeritageClauses != nil {
		for _, h := range cls.HeritageClauses.Nodes {
			hc := h.AsHeritageClause()
			if hc.Types == nil {
				continue
			}
			for _, t := range hc.Types.Nodes {
				var expr *ast.Node
				switch t.Kind {
				case ast.KindExpressionWithTypeArguments:
					expr = t.AsExpressionWithTypeArguments().Expression
				default:
					expr = t
				}
				if !IsPureExpr(src, expr) {
					return false
				}
			}
		}
	}
	if cls.Members == nil {
		return true
	}
	for _, mem := range cls.Members.Nodes {
		switch mem.Kind {
		case ast.KindClassStaticBlockDeclaration:
			return false
		case ast.KindPropertyDeclaration:
			if !ast.HasSyntacticModifier(mem, ast.ModifierFlagsStatic) {
				continue
			}
			init := mem.AsPropertyDeclaration().Initializer
			if init != nil && !IsPureExpr(src, init) {
				return false
			}
		}
	}
	return true
}

func IsPureExpr(src string, n *ast.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindNullKeyword, ast.KindTrueKeyword, ast.KindFalseKeyword,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindIdentifier, ast.KindThisKeyword:
		return true
	case ast.KindParenthesizedExpression:
		inner := n.AsParenthesizedExpression().Expression
		if hasPureAnnotation(src, n.Pos()) && isPureCallOrNew(src, inner) {
			return pureCallArgs(src, inner)
		}
		return IsPureExpr(src, inner)
	case ast.KindArrayLiteralExpression:
		list := n.AsArrayLiteralExpression().Elements
		if list == nil {
			return true
		}
		for _, el := range list.Nodes {
			if el.Kind != ast.KindSpreadElement && el.Kind != ast.KindOmittedExpression && !IsPureExpr(src, el) {
				return false
			}
			if el.Kind == ast.KindSpreadElement && !IsPureExpr(src, el.AsSpreadElement().Expression) {
				return false
			}
		}
		return true
	case ast.KindObjectLiteralExpression:
		list := n.AsObjectLiteralExpression().Properties
		if list == nil {
			return true
		}
		for _, prop := range list.Nodes {
			switch prop.Kind {
			case ast.KindPropertyAssignment:
				if !IsPureExpr(src, prop.AsPropertyAssignment().Initializer) {
					return false
				}
			case ast.KindShorthandPropertyAssignment:
				continue
			default:
				return false
			}
		}
		return true
	case ast.KindPropertyAccessExpression:
		return IsPureExpr(src, n.AsPropertyAccessExpression().Expression)
	case ast.KindCallExpression, ast.KindNewExpression:
		if hasPureAnnotation(src, n.Pos()) {
			return pureCallArgs(src, n)
		}
		if n.Kind == ast.KindCallExpression && isKnownPureCallee(n.AsCallExpression().Expression) {
			return pureCallArgs(src, n)
		}
		return false
	default:
		return false
	}
}

// isKnownPureCallee reports callees that are safe to treat as pure when args are pure.
func isKnownPureCallee(n *ast.Node) bool {
	if n == nil {
		return false
	}
	for n.Kind == ast.KindParenthesizedExpression {
		n = n.AsParenthesizedExpression().Expression
	}
	if n.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := n.AsPropertyAccessExpression()
	if access.Expression == nil || access.Expression.Kind != ast.KindIdentifier || access.Expression.Text() != "Object" {
		return false
	}
	name := access.Name()
	if name == nil {
		return false
	}
	switch name.Text() {
	case "assign", "create", "freeze", "keys", "values", "entries", "fromEntries", "is":
		return true
	default:
		return false
	}
}

func isPureCallOrNew(src string, n *ast.Node) bool {
	if n == nil {
		return false
	}
	for n.Kind == ast.KindParenthesizedExpression {
		n = n.AsParenthesizedExpression().Expression
	}
	return n.Kind == ast.KindCallExpression || n.Kind == ast.KindNewExpression
}

func pureCallArgs(src string, n *ast.Node) bool {
	if n == nil {
		return true
	}
	for n.Kind == ast.KindParenthesizedExpression {
		n = n.AsParenthesizedExpression().Expression
	}
	switch n.Kind {
	case ast.KindCallExpression:
		call := n.AsCallExpression()
		if call.Expression != nil && !IsPureExpr(src, call.Expression) {
			return false
		}
		for _, arg := range n.Arguments() {
			if !IsPureExpr(src, arg) {
				return false
			}
		}
		return true
	case ast.KindNewExpression:
		nw := n.AsNewExpression()
		if nw.Expression != nil && !IsPureExpr(src, nw.Expression) {
			return false
		}
		for _, arg := range n.Arguments() {
			if !IsPureExpr(src, arg) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// hasPureAnnotation reports whether a /*#__PURE__*/ or /*@__PURE__*/ comment
// is attached as leading trivia at pos (tsgo Node.Pos includes trivia) or
// sits immediately before the token when Pos already skips trivia.
func hasPureAnnotation(src string, pos int) bool {
	if pos < 0 || pos > len(src) {
		return false
	}
	tok := scanner.SkipTrivia(src, pos)
	if tok > len(src) {
		tok = len(src)
	}
	if commentHasPure(src[pos:tok]) {
		return true
	}
	// Pos may already be at the token; scan immediately preceding trivia.
	i := pos
	for i > 0 {
		i--
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		if c == '/' && i > 0 && src[i-1] == '*' {
			end := i + 1
			j := i - 1
			for j > 0 {
				if src[j] == '*' && src[j-1] == '/' {
					if commentHasPure(src[j-1 : end]) {
						return true
					}
					i = j - 1
					break
				}
				j--
			}
			if j <= 0 {
				return false
			}
			continue
		}
		return false
	}
	return false
}

func commentHasPure(s string) bool {
	return strings.Contains(s, "#__PURE__") || strings.Contains(s, "@__PURE__")
}

// ModuleSideEffectsFree reports whether file can be skipped when imported only
// for side effects, based on the nearest package.json "sideEffects" field.
func ModuleSideEffectsFree(file string) bool {
	pkgDir, side, ok := findSideEffectsField(file)
	if !ok {
		return false
	}
	switch v := side.(type) {
	case bool:
		return !v
	case []any:
		rel, err := filepath.Rel(pkgDir, file)
		if err != nil {
			return false
		}
		rel = filepath.ToSlash(rel)
		for _, item := range v {
			pat, ok := item.(string)
			if !ok {
				continue
			}
			if matchSideEffectPattern(pat, rel) {
				return false // this file has side effects
			}
		}
		return true // not listed => free of side effects
	default:
		return false
	}
}

func findSideEffectsField(file string) (pkgDir string, side any, ok bool) {
	dir := filepath.Dir(file)
	for {
		raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var meta struct {
				SideEffects any `json:"sideEffects"`
			}
			if json.Unmarshal(raw, &meta) != nil || meta.SideEffects == nil {
				return dir, nil, false
			}
			return dir, meta.SideEffects, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, false
		}
		dir = parent
	}
}

func matchSideEffectPattern(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	rel = filepath.ToSlash(rel)
	pattern = strings.TrimPrefix(pattern, "./")
	rel = strings.TrimPrefix(rel, "./")
	if pattern == rel {
		return true
	}
	if matched, _ := path.Match(pattern, rel); matched {
		return true
	}
	if matched, _ := path.Match(pattern, path.Base(rel)); matched {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		rest := strings.TrimPrefix(pattern, "**/")
		if matched, _ := path.Match(rest, path.Base(rel)); matched {
			return true
		}
		if matched, _ := path.Match(rest, rel); matched {
			return true
		}
		// any directory prefix
		parts := strings.Split(rel, "/")
		for i := range parts {
			sub := strings.Join(parts[i:], "/")
			if matched, _ := path.Match(rest, sub); matched {
				return true
			}
		}
	}
	// *.css matches nested paths ending with .css (common webpack shorthand)
	if strings.HasPrefix(pattern, "*.") && !strings.Contains(pattern, "/") {
		return strings.HasSuffix(rel, pattern[1:])
	}
	return false
}