package define

import (
	"slices"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

// rewrite collects the define and drop edits for one module. The edits never
// overlap: a dropped subtree is not visited again.
func rewrite(src string, rules map[string]rule, dropConsole, dropDebugger bool) []jsedit.Edit {
	file := jsedit.ParseEmittedJS(src)
	if file.Statements == nil {
		return nil
	}
	w := &rewriter{
		src:          src,
		rules:        rules,
		dropConsole:  dropConsole,
		dropDebugger: dropDebugger,
	}
	w.scopes = append(w.scopes, statementScope(src, file.Statements.Nodes))
	for _, stmt := range file.Statements.Nodes {
		w.visit(stmt)
	}
	return w.edits
}

type rewriter struct {
	rules map[string]rule
	src   string
	edits []jsedit.Edit
	// scopes is the lexical scope stack, innermost last. A define key is only
	// replaced where no enclosing scope declares the same name.
	scopes       []map[string]bool
	dropConsole  bool
	dropDebugger bool
}

func (w *rewriter) visit(n *ast.Node) {
	if n == nil {
		return
	}
	if names := scopeNames(w.src, n); names != nil {
		w.scopes = append(w.scopes, names)
		defer func() { w.scopes = w.scopes[:len(w.scopes)-1] }()
	}
	switch n.Kind {
	case ast.KindDebuggerStatement:
		if w.dropDebugger {
			w.removeStatement(n)
			return
		}
	case ast.KindExpressionStatement:
		if w.dropConsole && w.isConsoleCall(n.AsExpressionStatement().Expression) {
			w.removeStatement(n)
			return
		}
	case ast.KindCallExpression:
		if w.dropConsole && w.isConsoleCall(n) {
			w.replaceWithVoid(n)
			return
		}
	case ast.KindTaggedTemplateExpression:
		if w.dropConsole && w.isConsoleAccess(n.AsTaggedTemplateExpression().Tag) {
			w.replaceWithVoid(n)
			return
		}
	case ast.KindShorthandPropertyAssignment:
		if w.replaceShorthand(n) {
			return
		}
	}
	if len(w.rules) > 0 {
		if r, ok := w.match(n); ok {
			w.replace(n, r)
			return
		}
	}
	n.ForEachChild(func(child *ast.Node) bool {
		w.visit(child)
		return false
	})
}

// isConsoleCall reports whether expr is a console.* call.
func (w *rewriter) isConsoleCall(expr *ast.Node) bool {
	if expr == nil || expr.Kind != ast.KindCallExpression {
		return false
	}
	return w.isConsoleAccess(expr.AsCallExpression().Expression)
}

// isConsoleAccess reports whether n is a console.* member access whose console
// is not shadowed, so a local `console` object is left alone.
func (w *rewriter) isConsoleAccess(n *ast.Node) bool {
	if n == nil || n.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := n.AsPropertyAccessExpression()
	if access.QuestionDotToken != nil {
		return false
	}
	root := access.Expression
	if root == nil || root.Kind != ast.KindIdentifier {
		return false
	}
	return nodeText(w.src, root) == "console" && w.unbound(root)
}

// replaceWithVoid clears a console reference that sits inside an expression.
// The call's value is undefined and its side effects are gone, and the
// expression around it keeps working.
func (w *rewriter) replaceWithVoid(n *ast.Node) {
	text := voidExpr
	if tightPosition(n) {
		text = "(" + voidExpr + ")"
	}
	w.edits = append(w.edits, jsedit.Edit{
		Start: jsedit.TokenStart(w.src, n),
		End:   n.End(),
		Text:  text,
	})
}

// match returns the define rule that n is exactly. Property chains are matched
// as a whole so the longest key wins.
func (w *rewriter) match(n *ast.Node) (rule, bool) {
	switch n.Kind {
	case ast.KindIdentifier:
		r, ok := w.rules[nodeText(w.src, n)]
		if !ok || !w.freeReference(n) {
			return rule{}, false
		}
		return r, true
	case ast.KindPropertyAccessExpression, ast.KindMetaProperty:
		key, ok := w.chain(n)
		if !ok {
			return rule{}, false
		}
		r, ok := w.rules[key]
		return r, ok
	}
	return rule{}, false
}

// chain renders a static property chain ("process.env.NODE_ENV"). It fails for
// optional access, computed access, and roots that are not free identifiers.
func (w *rewriter) chain(n *ast.Node) (string, bool) {
	var parts []string
	for cur := n; cur != nil; {
		switch cur.Kind {
		case ast.KindPropertyAccessExpression:
			access := cur.AsPropertyAccessExpression()
			if access.QuestionDotToken != nil {
				return "", false
			}
			name := access.Name()
			if name == nil || name.Kind != ast.KindIdentifier {
				return "", false
			}
			parts = append([]string{nodeText(w.src, name)}, parts...)
			cur = access.Expression
		case ast.KindMetaProperty:
			meta := cur.AsMetaProperty()
			name := meta.Name()
			if name == nil {
				return "", false
			}
			head := "import"
			if meta.KeywordToken == ast.KindNewKeyword {
				head = "new"
			}
			return joinChain(head+"."+nodeText(w.src, name), parts), true
		case ast.KindIdentifier:
			if !w.freeReference(cur) {
				return "", false
			}
			return joinChain(nodeText(w.src, cur), parts), true
		case ast.KindThisKeyword:
			return joinChain("this", parts), true
		default:
			return "", false
		}
	}
	return "", false
}

func joinChain(root string, parts []string) string {
	if len(parts) == 0 {
		return root
	}
	var out strings.Builder
	out.WriteString(root)
	for _, part := range parts {
		out.WriteString("." + part)
	}
	return out.String()
}

func (w *rewriter) freeReference(n *ast.Node) bool {
	return isReferencePosition(n) && w.unbound(n)
}

// unbound reports whether no enclosing scope declares the identifier.
func (w *rewriter) unbound(n *ast.Node) bool {
	name := nodeText(w.src, n)
	for _, v := range slices.Backward(w.scopes) {
		if v[name] {
			return false
		}
	}
	return true
}

func (w *rewriter) replace(n *ast.Node, r rule) {
	if isWriteTarget(n) {
		return
	}
	w.edits = append(w.edits, jsedit.Edit{
		Start: jsedit.TokenStart(w.src, n),
		End:   n.End(),
		Text:  replacement(r),
	})
}

// replaceShorthand expands { __DEV__ } into { __DEV__: <value> }, because
// replacing only the name would also rename the property.
func (w *rewriter) replaceShorthand(n *ast.Node) bool {
	shorthand := n.AsShorthandPropertyAssignment()
	if shorthand.ObjectAssignmentInitializer != nil {
		return false
	}
	name := shorthand.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return false
	}
	key := nodeText(w.src, name)
	r, ok := w.rules[key]
	if !ok || !w.unbound(name) {
		return false
	}
	w.edits = append(w.edits, jsedit.Edit{
		Start: jsedit.TokenStart(w.src, n),
		End:   n.End(),
		Text:  key + ": " + replacement(r),
	})
	return true
}

func replacement(r rule) string {
	if r.wrap {
		return "(" + r.value + ")"
	}
	return r.value
}

// removeStatement deletes stmt, or replaces it with an empty block where the
// grammar requires a statement. The trivia before the statement goes too, so
// the surrounding lines stay free of blank holes; a comment there is kept and
// only the statement is removed.
func (w *rewriter) removeStatement(n *ast.Node) {
	start := n.Pos()
	tokenStart := jsedit.TokenStart(w.src, n)
	text := statementSeparator(w.src, n.End())
	if requiresStatementSlot(n) {
		// Keep the whitespace before the statement so the empty block stays
		// separated from the header.
		start = tokenStart
		text = "{}"
	} else if start < 0 || start > tokenStart || leadingComment(w.src[start:tokenStart]) {
		start = tokenStart
	}
	w.edits = append(w.edits, jsedit.Edit{Start: start, End: n.End(), Text: text})
}

// leadingComment reports whether trivia holds a comment.
func leadingComment(trivia string) bool {
	return strings.Contains(trivia, "//") || strings.Contains(trivia, "/*")
}

// statementSeparator returns the text that must remain at a deletion point so
// the next line cannot join the previous statement through automatic semicolon
// insertion.
func statementSeparator(src string, end int) string {
	if asiHazard(src, end) {
		return ";"
	}
	return ""
}

// asiHazard reports whether deleting text at end would let the following token
// continue the previous expression.
func asiHazard(src string, end int) bool {
	if end < 0 || end >= len(src) {
		return false
	}
	rest := src[end:]
	newline := false
	for i := 0; i < len(rest); {
		switch rest[i] {
		case ' ', '\t', '\r':
			i++
			continue
		case '\n':
			newline = true
			i++
			continue
		case '/':
			if i+1 < len(rest) && rest[i+1] == '/' {
				if nl := strings.IndexByte(rest[i:], '\n'); nl >= 0 {
					newline = true
					i += nl
					continue
				}
				return false
			}
			if i+1 < len(rest) && rest[i+1] == '*' {
				close := strings.Index(rest[i+2:], "*/")
				if close < 0 {
					return false
				}
				if strings.Contains(rest[i+2:i+2+close], "\n") {
					newline = true
				}
				i += close + 4
				continue
			}
		}
		if !newline {
			return false
		}
		switch rest[i] {
		case '(', '[', '`', '+', '-', '/':
			return true
		}
		return false
	}
	return false
}

// tightPosition reports whether a value at n binds tighter than a member, call,
// tag, or non-null expression, so the replacement must keep its parentheses.
func tightPosition(n *ast.Node) bool {
	parent := n.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		return parent.AsPropertyAccessExpression().Expression == n
	case ast.KindElementAccessExpression:
		return parent.AsElementAccessExpression().Expression == n
	case ast.KindCallExpression:
		return parent.AsCallExpression().Expression == n
	case ast.KindNewExpression:
		return parent.AsNewExpression().Expression == n
	case ast.KindTaggedTemplateExpression:
		return parent.AsTaggedTemplateExpression().Tag == n
	case ast.KindNonNullExpression:
		return true
	}
	return false
}

func requiresStatementSlot(n *ast.Node) bool {
	parent := n.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement,
		ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement,
		ast.KindWithStatement, ast.KindLabeledStatement:
		return true
	}
	return false
}

// isReferencePosition reports whether n is read as a value rather than used as
// a name: property keys, declaration names, labels, and import specifiers are
// not references.
func isReferencePosition(n *ast.Node) bool {
	parent := n.Parent
	if parent == nil {
		return true
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		return parent.AsPropertyAccessExpression().Name() != n
	case ast.KindQualifiedName:
		return parent.AsQualifiedName().Right != n
	case ast.KindPropertyAssignment:
		return parent.AsPropertyAssignment().Name() != n
	case ast.KindShorthandPropertyAssignment:
		return false
	case ast.KindLabeledStatement, ast.KindBreakStatement, ast.KindContinueStatement,
		ast.KindJsxAttribute:
		return false
	}
	return !isDeclarationName(parent, n)
}

// isDeclarationName reports whether parent stores n as its name. An initializer
// or a default value is a reference, so the node position decides.
func isDeclarationName(parent, n *ast.Node) bool {
	switch parent.Kind {
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement,
		ast.KindFunctionDeclaration, ast.KindFunctionExpression,
		ast.KindClassDeclaration, ast.KindClassExpression,
		ast.KindMethodDeclaration, ast.KindPropertyDeclaration,
		ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindEnumDeclaration, ast.KindEnumMember,
		ast.KindTypeAliasDeclaration, ast.KindInterfaceDeclaration,
		ast.KindModuleDeclaration, ast.KindTypeParameter,
		ast.KindImportSpecifier, ast.KindExportSpecifier, ast.KindImportClause,
		ast.KindNamespaceImport, ast.KindNamespaceExport,
		ast.KindImportEqualsDeclaration, ast.KindExportAssignment,
		ast.KindPropertySignature, ast.KindMethodSignature,
		ast.KindTypeReference:
		return parent.Name() == n
	}
	return false
}

// isWriteTarget reports whether n is assigned to. Replacing an assignment
// target with a value would produce an invalid assignment.
func isWriteTarget(n *ast.Node) bool {
	parent := n.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindBinaryExpression:
		bin := parent.AsBinaryExpression()
		return bin.Left == n && bin.OperatorToken != nil && isAssignmentOperator(bin.OperatorToken.Kind)
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return true
	case ast.KindDeleteExpression:
		return true
	}
	return false
}

func isAssignmentOperator(kind ast.Kind) bool {
	switch kind {
	case ast.KindEqualsToken,
		ast.KindPlusEqualsToken, ast.KindMinusEqualsToken,
		ast.KindAsteriskEqualsToken, ast.KindAsteriskAsteriskEqualsToken,
		ast.KindSlashEqualsToken, ast.KindPercentEqualsToken,
		ast.KindLessThanLessThanEqualsToken,
		ast.KindGreaterThanGreaterThanEqualsToken,
		ast.KindGreaterThanGreaterThanGreaterThanEqualsToken,
		ast.KindAmpersandEqualsToken, ast.KindBarEqualsToken,
		ast.KindCaretEqualsToken,
		ast.KindAmpersandAmpersandEqualsToken,
		ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return true
	}
	return false
}

// nodeText returns the source text of n without leading trivia.
func nodeText(src string, n *ast.Node) string {
	start := jsedit.TokenStart(src, n)
	end := n.End()
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return src[start:end]
}
