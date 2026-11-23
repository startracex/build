package link

import (
	"fmt"
	"slices"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/ast"
)

const (
	cjsExportsLocal = "__cjsExports"
	cjsRequireLocal = "__cjsRequire"
)

type cjsMode uint8

const (
	cjsNone cjsMode = iota
	cjsStatic
	cjsDynamic
)

type cjsProp struct {
	name string
	init *ast.Node
}

type cjsAnalysis struct {
	isCJS        bool
	mode         cjsMode
	skip         []*ast.Node
	props        []cjsProp
	defaultInit  *ast.Node
	okLHS        map[*ast.Node]bool
	wholesaleObj bool
}

// attachCJSExports registers CommonJS exports on m after a single parse.
// Static shapes become locals/exports without rewriting source. Unanalyzable
// shapes fall back to a __commonJS runtime wrapper.
func (m *esModule) attachCJSExports() {
	if m.file == nil || hasESMModuleSyntax(m.file) {
		return
	}
	a := analyzeCJS(m.file)
	if !a.isCJS {
		return
	}
	if a.mode == cjsDynamic {
		m.applyCJSDynamic()
		return
	}
	m.applyCJSStatic(a)
}

func (m *esModule) applyCJSStatic(a cjsAnalysis) {
	m.cjsMode = cjsStatic
	m.cjsSkip = make(map[*ast.Node]bool, len(a.skip))
	for _, stmt := range a.skip {
		m.cjsSkip[stmt] = true
	}

	if a.wholesaleObj && len(a.props) > 0 {
		for _, p := range a.props {
			m.locals[p.name] = &esLocal{name: p.name, cjsInit: p.init}
			m.exports[p.name] = &esExp{name: p.name, local: p.name}
			m.cjsNamedOrder = append(m.cjsNamedOrder, p.name)
		}
		m.locals[cjsExportsLocal] = &esLocal{name: cjsExportsLocal, cjsObjectBundle: true}
		m.exports["default"] = &esExp{name: "default", local: cjsExportsLocal}
		return
	}

	if a.defaultInit != nil {
		m.locals[cjsExportsLocal] = &esLocal{name: cjsExportsLocal, cjsInit: a.defaultInit}
		m.exports["default"] = &esExp{name: "default", local: cjsExportsLocal}
	}
	for _, p := range a.props {
		if m.locals[p.name] != nil {
			continue
		}
		m.locals[p.name] = &esLocal{name: p.name, cjsInit: p.init}
		m.exports[p.name] = &esExp{name: p.name, local: p.name}
		m.cjsNamedOrder = append(m.cjsNamedOrder, p.name)
	}
	if a.defaultInit == nil && len(a.props) > 0 {
		m.locals[cjsExportsLocal] = &esLocal{name: cjsExportsLocal, cjsObjectBundle: true}
		m.exports["default"] = &esExp{name: "default", local: cjsExportsLocal}
	}
}

func (m *esModule) applyCJSDynamic() {
	m.cjsMode = cjsDynamic
	m.cjsWrapAll = true
	m.locals[cjsRequireLocal] = &esLocal{name: cjsRequireLocal, cjsFactory: true}
	m.locals[cjsExportsLocal] = &esLocal{name: cjsExportsLocal, cjsFromFactory: true}
	m.exports["default"] = &esExp{name: "default", local: cjsExportsLocal}
}

func (m *esModule) ensureCJSProp(name string) {
	if name == "" || name == "default" || m.locals[name] != nil {
		return
	}
	if !m.cjsAllowsProp(name) {
		return
	}
	m.locals[name] = &esLocal{name: name, cjsFromExports: name}
	m.exports[name] = &esExp{name: name, local: name}
}

func (m *esModule) cjsAllowsProp(name string) bool {
	if m.cjsMode == cjsNone || !jsedit.IsIdentifierName(name) {
		return false
	}
	loc := m.locals[cjsExportsLocal]
	if loc == nil || loc.cjsObjectBundle {
		return false
	}
	return loc.cjsInit != nil || loc.cjsFromFactory
}

func analyzeCJS(file *ast.SourceFile) cjsAnalysis {
	a := cjsAnalysis{okLHS: map[*ast.Node]bool{}}
	if file.Statements == nil {
		return a
	}
	for _, stmt := range file.Statements.Nodes {
		if stmt.Kind != ast.KindExpressionStatement {
			continue
		}
		expr := stmt.AsExpressionStatement().Expression
		if expr == nil || expr.Kind != ast.KindBinaryExpression {
			continue
		}
		bin := expr.AsBinaryExpression()
		if bin.OperatorToken == nil || bin.OperatorToken.Kind != ast.KindEqualsToken {
			continue
		}
		left, right := bin.Left, bin.Right
		if left == nil || right == nil {
			continue
		}
		if isModuleExportsRef(left) {
			a.isCJS = true
			a.skip = append(a.skip, stmt)
			a.okLHS[left] = true
			if a.defaultInit != nil || len(a.props) > 0 {
				a.mode = cjsDynamic
			}
			a.defaultInit = right
			a.props = nil
			if right.Kind == ast.KindObjectLiteralExpression {
				props, ok := staticObjectProps(right)
				if ok {
					a.props = props
					a.wholesaleObj = true
				} else {
					a.mode = cjsDynamic
				}
			} else {
				a.wholesaleObj = false
			}
			continue
		}
		if prop, ok := exportsPropName(left); ok {
			a.isCJS = true
			a.skip = append(a.skip, stmt)
			a.okLHS[left] = true
			if a.defaultInit != nil {
				a.mode = cjsDynamic
			}
			a.wholesaleObj = false
			a.props = append(a.props, cjsProp{name: prop, init: right})
			continue
		}
		if isExportsElementAssign(left) {
			a.isCJS = true
			a.mode = cjsDynamic
		}
	}
	if cjsFileUsesExports(file) {
		a.isCJS = true
	}
	if !a.isCJS {
		return a
	}
	if a.mode == cjsDynamic || cjsHasDynamicUse(file, a.okLHS) {
		a.mode = cjsDynamic
		return a
	}
	if a.defaultInit == nil && len(a.props) == 0 {
		a.mode = cjsDynamic
		return a
	}
	a.mode = cjsStatic
	return a
}

func isExportsElementAssign(left *ast.Node) bool {
	if left == nil || left.Kind != ast.KindElementAccessExpression {
		return false
	}
	expr := left.AsElementAccessExpression().Expression
	if expr == nil {
		return false
	}
	if expr.Kind == ast.KindIdentifier && expr.Text() == "exports" {
		return true
	}
	return isModuleExportsRef(expr)
}

func cjsFileUsesExports(file *ast.SourceFile) bool {
	found := false
	var walk func(n *ast.Node)
	walk = func(n *ast.Node) {
		if n == nil || found {
			return
		}
		if isModuleExportsRef(n) || (n.Kind == ast.KindIdentifier && n.Text() == "exports") {
			found = true
			return
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return found
		})
	}
	if file.Statements != nil {
		for _, stmt := range file.Statements.Nodes {
			walk(stmt)
			if found {
				return true
			}
		}
	}
	return false
}

func staticObjectProps(obj *ast.Node) ([]cjsProp, bool) {
	list := obj.AsObjectLiteralExpression().Properties
	if list == nil {
		return nil, true
	}
	var props []cjsProp
	for _, prop := range list.Nodes {
		switch prop.Kind {
		case ast.KindPropertyAssignment:
			pa := prop.AsPropertyAssignment()
			name, ok := staticPropName(pa.Name())
			if !ok || pa.Initializer == nil {
				return nil, false
			}
			props = append(props, cjsProp{name: name, init: pa.Initializer})
		case ast.KindShorthandPropertyAssignment:
			nameNode := prop.AsShorthandPropertyAssignment().Name()
			if nameNode == nil || !jsedit.IsIdentifierName(nameNode.Text()) {
				return nil, false
			}
			props = append(props, cjsProp{name: nameNode.Text(), init: nameNode})
		default:
			return nil, false
		}
	}
	return props, true
}

func cjsHasDynamicUse(file *ast.SourceFile, okLHS map[*ast.Node]bool) bool {
	dynamic := false
	var walk func(n, parent *ast.Node)
	walk = func(n, parent *ast.Node) {
		if n == nil || dynamic {
			return
		}
		if isModuleExportsRef(n) {
			if !okLHS[n] {
				dynamic = true
			}
		} else if n.Kind == ast.KindIdentifier && n.Text() == "exports" {
			ok := false
			if parent != nil && parent.Kind == ast.KindPropertyAccessExpression {
				access := parent.AsPropertyAccessExpression()
				if access.Name() == n {
					ok = true // module.exports name token
				} else if access.Expression == n && okLHS[parent] {
					ok = true // exports.x assignment LHS
				}
			}
			if !ok {
				dynamic = true
			}
		} else if n.Kind == ast.KindElementAccessExpression {
			elem := n.AsElementAccessExpression()
			if elem.Expression != nil && (isModuleExportsRef(elem.Expression) ||
				(elem.Expression.Kind == ast.KindIdentifier && elem.Expression.Text() == "exports")) {
				dynamic = true
			}
		}
		if dynamic {
			return
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child, n)
			return dynamic
		})
	}
	if file.Statements != nil {
		for _, stmt := range file.Statements.Nodes {
			walk(stmt, nil)
			if dynamic {
				return true
			}
		}
	}
	return false
}

func hasESMModuleSyntax(file *ast.SourceFile) bool {
	if file.Statements == nil {
		return false
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration, ast.KindExportAssignment:
			return true
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindVariableStatement:
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				return true
			}
		}
	}
	return false
}

func isModuleExportsRef(n *ast.Node) bool {
	if n == nil || n.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := n.AsPropertyAccessExpression()
	if access.Expression == nil || access.Name() == nil {
		return false
	}
	return access.Expression.Kind == ast.KindIdentifier &&
		access.Expression.Text() == "module" &&
		access.Name().Text() == "exports"
}

func exportsPropName(n *ast.Node) (string, bool) {
	if n == nil || n.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}
	access := n.AsPropertyAccessExpression()
	if access.Expression == nil || access.Name() == nil {
		return "", false
	}
	name := access.Name().Text()
	if !jsedit.IsIdentifierName(name) {
		return "", false
	}
	if access.Expression.Kind == ast.KindIdentifier && access.Expression.Text() == "exports" {
		return name, true
	}
	if isModuleExportsRef(access.Expression) {
		return name, true
	}
	return "", false
}

func staticPropName(name *ast.Node) (string, bool) {
	if name == nil {
		return "", false
	}
	switch name.Kind {
	case ast.KindIdentifier:
		if jsedit.IsIdentifierName(name.Text()) {
			return name.Text(), true
		}
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		text := name.Text()
		if jsedit.IsIdentifierName(text) {
			return text, true
		}
	}
	return "", false
}

func commonJSHelperSource(name string) string {
	return "var " + name + ` = (cb) => {
  var mod;
  return () => {
    if (!mod) {
      mod = { exports: {} };
      cb(mod.exports, mod);
    }
    return mod.exports;
  };
};`
}

func (m *esModule) renderCJSBindings(mods map[string]*esModule, helper string) []string {
	switch m.cjsMode {
	case cjsDynamic:
		return m.renderCJSDynamic(helper)
	case cjsStatic:
		return m.renderCJSStatic(mods)
	default:
		return nil
	}
}

func (m *esModule) renderCJSDynamic(helper string) []string {
	req := m.locals[cjsRequireLocal]
	exp := m.locals[cjsExportsLocal]
	if req == nil || exp == nil {
		return nil
	}
	if !m.liveLoc[cjsRequireLocal] && !m.liveLoc[cjsExportsLocal] {
		return nil
	}
	if helper == "" {
		helper = "__commonJS"
	}
	var out []string
	body := strings.TrimRight(m.src, "\n")
	out = append(out, fmt.Sprintf("var %s = %s((exports, module) => {\n%s\n});", req.final, helper, body))
	if m.liveLoc[cjsExportsLocal] && exp.final != "" {
		out = append(out, fmt.Sprintf("const %s = %s();", exp.final, req.final))
	}
	out = append(out, m.renderCJSPropLocals()...)
	return out
}

func (m *esModule) renderCJSStatic(mods map[string]*esModule) []string {
	_ = mods
	seen := map[string]bool{}
	var ordered []string
	for _, name := range m.cjsNamedOrder {
		if m.liveLoc[name] && m.locals[name] != nil && m.locals[name].cjsInit != nil {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	var rest []string
	for name, loc := range m.locals {
		if !m.liveLoc[name] || loc.cjsInit == nil || loc.cjsObjectBundle || seen[name] {
			continue
		}
		rest = append(rest, name)
	}
	slices.Sort(rest)
	ordered = append(ordered, rest...)

	var out []string
	for _, name := range ordered {
		loc := m.locals[name]
		if loc.final == "" || loc.cjsInit == nil {
			continue
		}
		out = append(out, fmt.Sprintf("const %s = %s;", loc.final, m.renderExpr(loc.cjsInit)))
	}
	if loc := m.locals[cjsExportsLocal]; loc != nil && loc.cjsObjectBundle && m.liveLoc[cjsExportsLocal] && loc.final != "" {
		var props []string
		for _, name := range m.cjsNamedOrder {
			if !m.liveLoc[name] {
				continue
			}
			binding := m.locals[name]
			if binding == nil || binding.final == "" {
				continue
			}
			props = append(props, fmt.Sprintf("  %s: %s,", name, binding.final))
		}
		out = append(out, fmt.Sprintf("const %s = {\n%s\n};", loc.final, strings.Join(props, "\n")))
	}
	out = append(out, m.renderCJSPropLocals()...)
	return out
}

func (m *esModule) renderCJSPropLocals() []string {
	var names []string
	for name, loc := range m.locals {
		if loc.cjsFromExports == "" || !m.liveLoc[name] || loc.final == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	exp := m.locals[cjsExportsLocal]
	if exp == nil || exp.final == "" {
		return nil
	}
	var out []string
	for _, name := range names {
		loc := m.locals[name]
		out = append(out, fmt.Sprintf("const %s = %s.%s;", loc.final, exp.final, loc.cjsFromExports))
	}
	return out
}

func (m *esModule) renderExpr(n *ast.Node) string {
	if n == nil {
		return ""
	}
	start, end := n.Pos(), n.End()
	if start < 0 || end <= start || end > len(m.src) {
		return ""
	}
	var edits []jsedit.Edit
	m.walkRefs(n, nil, m.moduleScope(), func(r ref) {
		if r.kind != refName || r.node == nil {
			return
		}
		final, ok := m.outputName(r.name)
		if !ok || final == r.node.Text() {
			return
		}
		tok := jsedit.TokenStart(m.src, r.node)
		edits = append(edits, jsedit.Edit{Start: tok - start, End: r.node.End() - start, Text: final})
	})
	chunk := m.src[start:end]
	if len(edits) == 0 {
		return strings.TrimSpace(chunk)
	}
	out, err := jsedit.ApplyEdits(chunk, edits)
	if err != nil {
		return strings.TrimSpace(chunk)
	}
	return strings.TrimSpace(out)
}

func cjsFactoryPreferredName(id string) string {
	base := modpath.BaseStem(id)
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, base)
	if base == "" || base[0] >= '0' && base[0] <= '9' {
		base = "mod_" + base
	}
	return "require_" + base
}

func anyCJSDynamic(mods []*esModule) bool {
	for _, m := range mods {
		if m.cjsMode == cjsDynamic && (m.liveLoc[cjsRequireLocal] || m.liveLoc[cjsExportsLocal]) {
			return true
		}
	}
	return false
}

func anyCJSDynamicMap(mods map[string]*esModule) bool {
	for _, m := range mods {
		if m.cjsMode == cjsDynamic && (m.liveLoc[cjsRequireLocal] || m.liveLoc[cjsExportsLocal]) {
			return true
		}
	}
	return false
}
