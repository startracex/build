package bundler

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/scanner"
)

// linkESM scope-hoists the module graph into one ECMAScript module.
// Local imports are inlined. Node built-ins and other externals stay as
// import declarations. Side-effect-free exports that nothing uses are omitted.
func linkESM(b *bundler, entryID string) (string, error) {
	mods := map[string]*esModule{}
	var order []string
	for id, unit := range b.units {
		mod, err := parseLinkedModule(b, id, unit.code)
		if err != nil {
			return "", err
		}
		mod.effectsFree = moduleSideEffectsFree(mod.osPath())
		mods[id] = mod
		order = append(order, id)
	}
	slices.Sort(order)
	entry := mods[entryID]
	if entry == nil {
		return "", fmt.Errorf("missing entry module")
	}
	if err := markLive(mods, entry); err != nil {
		return "", err
	}
	names := &namer{used: map[string]int{}, taken: map[string]struct{}{}}
	helper := ""
	if anyCJSDynamicMap(mods) {
		// Reserve before module locals so a user binding named __commonJS
		// is renamed instead of colliding with the runtime helper.
		helper = names.take("__commonJS")
	}
	if err := assignNames(mods, order, names); err != nil {
		return "", err
	}
	return renderLinked(mods, order, entry, b.format, helper), nil
}

type esModule struct {
	id            string
	src           string
	file          *ast.SourceFile
	locals        map[string]*esLocal
	imports       map[string]*esImport // local name -> import
	exports       map[string]*esExp
	stars         []string // module ids re-exported with export *
	sideImps      []string
	needed        []string
	inst          bool
	liveExp       map[string]bool
	liveLoc       map[string]bool
	extFinal      map[extKey]string
	b             *bundler
	effectsFree   bool
	cjsMode       cjsMode
	cjsSkip       map[*ast.Node]bool
	cjsWrapAll    bool
	cjsNamedOrder []string
}

type extKey struct{ spec, name string }

type esLocal struct {
	name            string
	idents          []*ast.Node
	stmt            *ast.Node
	final           string
	defExpr         bool // synthetic const for `export default <expr>`
	cjsInit         *ast.Node
	cjsFromExports  string // const x = __cjsExports.x
	cjsObjectBundle bool   // const __cjsExports = { a, b }
	cjsFactory      bool   // var require_x = __commonJS(...)
	cjsFromFactory  bool   // const __cjsExports = require_x()
}

type esImport struct {
	local     string
	spec      string
	module    string // empty if external
	external  bool
	imported  string // export name, "*" for namespace
	escapes   bool
	props     map[string]bool
	namespace bool
	final     string
	used      bool
}

type esExp struct {
	name     string
	local    string
	from     string
	fromName string
	starNS   bool // export * as ns
}

type namer struct {
	used  map[string]int      // base -> allocation attempts
	taken map[string]struct{} // final names already issued
}

func (n *namer) take(preferred string) string {
	base := preferred
	if !isIdentifierName(base) {
		base = "binding"
	}
	for {
		n.used[base]++
		name := base
		if n.used[base] > 1 {
			name = fmt.Sprintf("%s$%d", base, n.used[base])
		}
		if _, ok := n.taken[name]; ok {
			// e.g. preferred "foo$2" already took that exact name
			continue
		}
		n.taken[name] = struct{}{}
		return name
	}
}

func parseLinkedModule(b *bundler, id, src string) (*esModule, error) {
	file := parseEmittedJS(src)
	if err := errorFromDiagnostics(id, src, file.Diagnostics()); err != nil {
		return nil, err
	}
	m := &esModule{
		id:      id,
		src:     src,
		file:    file,
		locals:  map[string]*esLocal{},
		imports: map[string]*esImport{},
		exports: map[string]*esExp{},
		liveExp: map[string]bool{},
		liveLoc: map[string]bool{},
		b:       b,
	}
	if file.Statements != nil {
		for _, stmt := range file.Statements.Nodes {
			if err := m.declareStmt(b, stmt); err != nil {
				return nil, err
			}
		}
	}
	m.attachCJSExports()
	return m, nil
}

func (m *esModule) declareStmt(b *bundler, stmt *ast.Node) error {
	switch stmt.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		return m.declareImport(b, stmt)
	case ast.KindExportDeclaration:
		return m.declareExport(b, stmt)
	case ast.KindExportAssignment:
		exp := stmt.AsExportAssignment()
		if exp.Expression != nil && exp.Expression.Kind == ast.KindIdentifier && !exp.IsExportEquals {
			m.exports["default"] = &esExp{name: "default", local: exp.Expression.Text()}
			return nil
		}
		local := &esLocal{name: "default", stmt: stmt, defExpr: true}
		m.locals["default"] = local
		m.exports["default"] = &esExp{name: "default", local: "default"}
		return nil
	case ast.KindFunctionDeclaration:
		if name := stmt.AsFunctionDeclaration().Name(); name != nil {
			m.addLocal(name.Text(), stmt, name)
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				exported := name.Text()
				if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsDefault) {
					exported = "default"
				}
				m.exports[exported] = &esExp{name: exported, local: name.Text()}
			}
		}
	case ast.KindClassDeclaration:
		if name := stmt.AsClassDeclaration().Name(); name != nil {
			m.addLocal(name.Text(), stmt, name)
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				exported := name.Text()
				if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsDefault) {
					exported = "default"
				}
				m.exports[exported] = &esExp{name: exported, local: name.Text()}
			}
		}
	case ast.KindVariableStatement:
		m.declareVars(stmt)
	}
	return nil
}

func (m *esModule) addLocal(name string, stmt, ident *ast.Node) {
	loc := m.locals[name]
	if loc == nil {
		loc = &esLocal{name: name, stmt: stmt}
		m.locals[name] = loc
	}
	if ident != nil {
		loc.idents = append(loc.idents, ident)
	}
	if loc.stmt == nil {
		loc.stmt = stmt
	}
}

func (m *esModule) declareVars(stmt *ast.Node) {
	listNode := stmt.AsVariableStatement().DeclarationList
	if listNode == nil {
		return
	}
	list := listNode.AsVariableDeclarationList()
	if list.Declarations == nil {
		return
	}
	exported := ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport)
	for _, declNode := range list.Declarations.Nodes {
		decl := declNode.AsVariableDeclaration()
		eachBindingIdent(decl.Name(), func(id *ast.Node) {
			m.addLocal(id.Text(), stmt, id)
			if exported {
				m.exports[id.Text()] = &esExp{name: id.Text(), local: id.Text()}
			}
		})
	}
}

func (m *esModule) declareImport(b *bundler, stmt *ast.Node) error {
	decl := stmt.AsImportDeclaration()
	spec := specifierText(decl.ModuleSpecifier)
	resolved, external, err := b.resolve(m.osPath(), spec)
	if err != nil {
		return err
	}
	chunkExt := ""
	if !external && resolved != "" && b.chunkPaths != nil {
		if cp := b.chunkPaths[moduleID(resolved)]; cp != "" && cp != b.linkingChunk {
			external = true
			chunkExt = "./" + cp
		}
	}
	if decl.ImportClause == nil {
		if !external {
			m.sideImps = append(m.sideImps, moduleID(resolved))
		}
		return nil
	}
	clause := decl.ImportClause.AsImportClause()
	if clause.PhaseModifier == ast.KindTypeKeyword {
		return nil
	}
	add := func(local, imported string, namespace bool) {
		useSpec := spec
		if chunkExt != "" {
			useSpec = chunkExt
		}
		m.imports[local] = &esImport{
			local:     local,
			spec:      useSpec,
			module:    moduleID(resolved),
			external:  external,
			imported:  imported,
			namespace: namespace,
			props:     map[string]bool{},
		}
		if external {
			m.imports[local].module = ""
		}
	}
	if clause.Name() != nil {
		add(clause.Name().Text(), "default", false)
	}
	if clause.NamedBindings == nil {
		return nil
	}
	switch clause.NamedBindings.Kind {
	case ast.KindNamespaceImport:
		add(clause.NamedBindings.AsNamespaceImport().Name().Text(), "*", true)
	case ast.KindNamedImports:
		elements := clause.NamedBindings.AsNamedImports().Elements
		if elements == nil {
			return nil
		}
		for _, el := range elements.Nodes {
			spec := el.AsImportSpecifier()
			if spec.IsTypeOnly || spec.Name() == nil {
				continue
			}
			local := spec.Name().Text()
			imported := local
			if spec.PropertyName != nil {
				imported = spec.PropertyName.Text()
			}
			add(local, imported, false)
		}
	}
	return nil
}

func (m *esModule) declareExport(b *bundler, stmt *ast.Node) error {
	decl := stmt.AsExportDeclaration()
	if decl.IsTypeOnly {
		return nil
	}
	if decl.ModuleSpecifier == nil && decl.ExportClause != nil && decl.ExportClause.Kind == ast.KindNamedExports {
		elements := decl.ExportClause.AsNamedExports().Elements
		if elements == nil {
			return nil
		}
		for _, el := range elements.Nodes {
			spec := el.AsExportSpecifier()
			if spec.IsTypeOnly || spec.Name() == nil {
				continue
			}
			exported := spec.Name().Text()
			local := exported
			if spec.PropertyName != nil {
				local = spec.PropertyName.Text()
			}
			m.exports[exported] = &esExp{name: exported, local: local}
		}
		return nil
	}
	if decl.ModuleSpecifier == nil {
		return nil
	}
	spec := specifierText(decl.ModuleSpecifier)
	resolved, external, err := b.resolve(m.osPath(), spec)
	if err != nil {
		return err
	}
	from := ""
	if !external {
		from = moduleID(resolved)
	}
	if decl.ExportClause == nil {
		if external {
			return fmt.Errorf("cannot re-export * from external module %s", strconv.Quote(spec))
		}
		m.stars = append(m.stars, from)
		return nil
	}
	switch decl.ExportClause.Kind {
	case ast.KindNamespaceExport:
		name := decl.ExportClause.AsNamespaceExport().Name().Text()
		if external {
			m.exports[name] = &esExp{name: name, fromName: "*", from: spec}
			m.exports[name].starNS = true
			// external namespace is represented as an import
			m.imports[name] = &esImport{
				local: name, spec: spec, external: true, imported: "*", namespace: true,
				props: map[string]bool{},
			}
			return nil
		}
		m.exports[name] = &esExp{name: name, from: from, fromName: "*", starNS: true}
	case ast.KindNamedExports:
		elements := decl.ExportClause.AsNamedExports().Elements
		if elements == nil {
			return nil
		}
		for _, el := range elements.Nodes {
			specNode := el.AsExportSpecifier()
			if specNode.IsTypeOnly || specNode.Name() == nil {
				continue
			}
			exported := specNode.Name().Text()
			orig := exported
			if specNode.PropertyName != nil {
				orig = specNode.PropertyName.Text()
			}
			if external {
				m.exports[exported] = &esExp{name: exported, from: spec, fromName: orig}
				continue
			}
			m.exports[exported] = &esExp{name: exported, from: from, fromName: orig}
		}
	}
	return nil
}

func (m *esModule) osPath() string {
	return filepath.FromSlash(m.id)
}

type expTarget struct {
	mod     *esModule
	local   string
	extSpec string
	extName string
	extNS   bool
}

func markLive(mods map[string]*esModule, entry *esModule) error {
	// instantiate walks side-effect imports (import "./x.js") and scans their
	// impure statements; markLive used to set inst and only scan the entry.
	if err := entry.instantiate(mods); err != nil {
		return err
	}
	names, err := entry.publicNames(mods, map[string]bool{})
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := markExport(mods, entry, name, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func (m *esModule) publicNames(mods map[string]*esModule, stack map[string]bool) ([]string, error) {
	if stack[m.id] {
		return nil, nil
	}
	stack[m.id] = true
	defer delete(stack, m.id)
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range m.exports {
		add(name)
	}
	for _, star := range m.stars {
		dep := mods[star]
		if dep == nil {
			continue
		}
		nested, err := dep.publicNames(mods, stack)
		if err != nil {
			return nil, err
		}
		for _, name := range nested {
			if name == "default" {
				continue
			}
			if _, explicit := m.exports[name]; explicit {
				continue
			}
			add(name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func markExport(mods map[string]*esModule, m *esModule, name string, stack map[string]bool) error {
	key := m.id + "\x00" + name
	if stack[key] {
		return nil
	}
	stack[key] = true
	if m.liveExp[name] {
		return nil
	}
	target, ok, err := m.resolveExport(mods, name, map[string]bool{})
	if err != nil || !ok {
		return err
	}
	m.liveExp[name] = true
	if err := m.instantiate(mods); err != nil {
		return err
	}
	if target.mod != nil && target.local != "" {
		m.need(target.mod.id)
		return markLocal(mods, target.mod, target.local)
	}
	return nil
}

func (m *esModule) need(id string) {
	if id == "" || id == m.id {
		return
	}
	for _, existing := range m.needed {
		if existing == id {
			return
		}
	}
	m.needed = append(m.needed, id)
}

func markLocal(mods map[string]*esModule, m *esModule, name string) error {
	if m.liveLoc[name] {
		return nil
	}
	m.liveLoc[name] = true
	if err := m.instantiate(mods); err != nil {
		return err
	}
	loc := m.locals[name]
	if loc == nil {
		return nil
	}
	switch {
	case loc.cjsFromExports != "":
		return markLocal(mods, m, cjsExportsLocal)
	case loc.cjsFromFactory:
		return markLocal(mods, m, cjsRequireLocal)
	case loc.cjsObjectBundle:
		for _, n := range m.cjsNamedOrder {
			if err := markLocal(mods, m, n); err != nil {
				return err
			}
		}
		return nil
	case loc.cjsFactory:
		return nil
	case loc.cjsInit != nil:
		return m.scanNode(mods, loc.cjsInit)
	case loc.stmt != nil:
		return m.scanNode(mods, loc.stmt)
	default:
		return nil
	}
}

func (m *esModule) instantiate(mods map[string]*esModule) error {
	if m.inst {
		return nil
	}
	m.inst = true
	for _, id := range m.sideImps {
		dep := mods[id]
		if dep == nil || dep.effectsFree {
			continue
		}
		if err := dep.instantiate(mods); err != nil {
			return err
		}
	}
	return m.scanSideEffects(mods)
}

func (m *esModule) scanSideEffects(mods map[string]*esModule) error {
	if m.file == nil || m.file.Statements == nil {
		return nil
	}
	for _, stmt := range m.file.Statements.Nodes {
		if m.cjsSkip[stmt] {
			continue
		}
		if isSkippableStmt(m.src, stmt) || isPureDecl(m.src, stmt) {
			continue
		}
		if err := m.scanNode(mods, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (m *esModule) scanNode(mods map[string]*esModule, root *ast.Node) error {
	var scanErr error
	m.walkRefs(root, nil, m.moduleScope(), func(r ref) {
		if scanErr != nil {
			return
		}
		scanErr = m.applyRef(mods, r)
	})
	return scanErr
}

func (m *esModule) applyRef(mods map[string]*esModule, r ref) error {
	switch r.kind {
	case refName:
		if imp, ok := m.imports[r.name]; ok {
			return m.markImport(mods, imp, "")
		}
		if _, ok := m.locals[r.name]; ok {
			return markLocal(mods, m, r.name)
		}
	case refNSProp:
		imp := m.imports[r.name]
		if imp == nil {
			return nil
		}
		imp.props[r.prop] = true
		return m.markImport(mods, imp, r.prop)
	case refNSValue:
		imp := m.imports[r.name]
		if imp == nil {
			return nil
		}
		imp.escapes = true
		return m.markImport(mods, imp, "*")
	case refDynamic:
		if r.external {
			return nil
		}
		if m.b != nil {
			if _, ok := m.b.splitDynamicImport(r.module); ok {
				return nil
			}
		}
		m.need(r.module)
		dep := mods[r.module]
		if dep == nil {
			return nil
		}
		names, err := dep.publicNames(mods, map[string]bool{})
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := markExport(mods, dep, name, map[string]bool{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *esModule) resolveExport(mods map[string]*esModule, name string, stack map[string]bool) (expTarget, bool, error) {
	key := m.id + "\x00" + name
	if stack[key] {
		return expTarget{}, false, nil
	}
	stack[key] = true
	if exp, ok := m.exports[name]; ok {
		if exp.local != "" {
			if imp, isImp := m.imports[exp.local]; isImp && exp.from == "" {
				if imp.external {
					return expTarget{extSpec: imp.spec, extName: imp.imported, extNS: imp.namespace}, true, nil
				}
				dep := mods[imp.module]
				if dep == nil {
					return expTarget{}, false, fmt.Errorf("missing module for %s", imp.spec)
				}
				return dep.resolveExport(mods, imp.imported, stack)
			}
			return expTarget{mod: m, local: exp.local}, true, nil
		}
		if mods[exp.from] == nil && exp.fromName != "" {
			spec := exp.from
			if exp.starNS {
				return expTarget{extSpec: spec, extName: "*", extNS: true}, true, nil
			}
			return expTarget{extSpec: spec, extName: exp.fromName}, true, nil
		}
		dep := mods[exp.from]
		if dep == nil {
			return expTarget{}, false, fmt.Errorf("cannot resolve export %s", name)
		}
		if exp.fromName == "*" {
			return expTarget{}, false, fmt.Errorf("namespace re-export %s is not a value export", name)
		}
		return dep.resolveExport(mods, exp.fromName, stack)
	}
	if name != "default" && m.cjsAllowsProp(name) {
		m.ensureCJSProp(name)
		if exp, ok := m.exports[name]; ok && exp.local != "" {
			return expTarget{mod: m, local: exp.local}, true, nil
		}
	}
	if name == "default" {
		return expTarget{}, false, nil
	}
	for _, star := range m.stars {
		dep := mods[star]
		if dep == nil {
			continue
		}
		target, ok, err := dep.resolveExport(mods, name, stack)
		if err != nil || ok {
			return target, ok, err
		}
	}
	return expTarget{}, false, nil
}

type refKind int

const (
	refName refKind = iota
	refNSProp
	refNSValue
	refDynamic
)

type ref struct {
	kind      refKind
	name      string
	prop      string
	module    string
	external  bool
	node      *ast.Node
	shorthand bool
}

type scope struct {
	names  map[string]bool
	parent *scope
	module bool
}

func (s *scope) define(name string) {
	if name != "" {
		s.names[name] = true
	}
}

func (s *scope) shadows(name string) bool {
	for s != nil && !s.module {
		if s.names[name] {
			return true
		}
		s = s.parent
	}
	return false
}

func (m *esModule) moduleScope() *scope {
	sc := &scope{names: map[string]bool{}, module: true}
	for name := range m.locals {
		sc.define(name)
	}
	return sc
}

func (m *esModule) walkRefs(n, parent *ast.Node, sc *scope, fn func(ref)) {
	if n == nil || sc == nil {
		return
	}
	switch n.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration:
		return
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
		m.walkFunction(n, sc, fn)
		return
	case ast.KindBlock:
		m.walkBlock(n, sc, fn)
		return
	case ast.KindCatchClause:
		clause := n.AsCatchClause()
		inner := &scope{names: map[string]bool{}, parent: sc}
		if clause.VariableDeclaration != nil {
			eachBindingIdent(clause.VariableDeclaration.AsVariableDeclaration().Name(), func(id *ast.Node) {
				inner.define(id.Text())
			})
		}
		if clause.Block != nil {
			m.walkBlock(clause.Block, inner, fn)
		}
		return
	case ast.KindForStatement:
		stmt := n.AsForStatement()
		inner := &scope{names: map[string]bool{}, parent: sc}
		defineLexical(stmt.Initializer, inner)
		m.walk(stmt.Initializer, n, inner, fn)
		m.walk(stmt.Condition, n, inner, fn)
		m.walk(stmt.Incrementor, n, inner, fn)
		m.walk(stmt.Statement, n, inner, fn)
		return
	case ast.KindForInStatement, ast.KindForOfStatement:
		stmt := n.AsForInOrOfStatement()
		inner := &scope{names: map[string]bool{}, parent: sc}
		defineLexical(stmt.Initializer, inner)
		m.walk(stmt.Initializer, n, inner, fn)
		m.walk(stmt.Expression, n, inner, fn)
		m.walk(stmt.Statement, n, inner, fn)
		return
	case ast.KindIdentifier:
		m.noteIdent(n, parent, sc, fn)
		return
	case ast.KindPropertyAccessExpression:
		m.walkAccess(n, sc, fn)
		return
	case ast.KindElementAccessExpression:
		m.walkElement(n, sc, fn)
		return
	case ast.KindCallExpression:
		call := n.AsCallExpression()
		if call.Expression != nil && call.Expression.Kind == ast.KindImportKeyword {
			m.noteDynamic(n, fn)
			return
		}
	case ast.KindBindingElement:
		el := n.AsBindingElement()
		if el.Initializer != nil {
			m.walk(el.Initializer, n, sc, fn)
		}
		return
	}
	n.ForEachChild(func(child *ast.Node) bool {
		m.walkRefs(child, n, sc, fn)
		return false
	})
}

func (m *esModule) walk(n, parent *ast.Node, sc *scope, fn func(ref)) {
	m.walkRefs(n, parent, sc, fn)
}

func (m *esModule) walkFunction(n *ast.Node, sc *scope, fn func(ref)) {
	fnScope := &scope{names: map[string]bool{}, parent: sc}
	for _, p := range n.Parameters() {
		eachBindingIdent(p.AsParameterDeclaration().Name(), func(id *ast.Node) { fnScope.define(id.Text()) })
	}
	if n.Kind == ast.KindFunctionExpression {
		if name := n.AsFunctionExpression().Name(); name != nil {
			fnScope.define(name.Text())
		}
	}
	if body := n.Body(); body != nil {
		collectVars(body, fnScope)
		if body.Kind == ast.KindBlock {
			m.walkBlock(body, fnScope, fn)
			return
		}
		m.walk(body, n, fnScope, fn)
	}
}

func (m *esModule) walkBlock(n *ast.Node, sc *scope, fn func(ref)) {
	block := n.AsBlock()
	inner := &scope{names: map[string]bool{}, parent: sc}
	if block.Statements != nil {
		for _, stmt := range block.Statements.Nodes {
			defineLexical(stmt, inner)
			if stmt.Kind == ast.KindFunctionDeclaration {
				if name := stmt.AsFunctionDeclaration().Name(); name != nil {
					inner.define(name.Text())
				}
			}
			if stmt.Kind == ast.KindClassDeclaration {
				if name := stmt.AsClassDeclaration().Name(); name != nil {
					inner.define(name.Text())
				}
			}
		}
		for _, stmt := range block.Statements.Nodes {
			m.walk(stmt, n, inner, fn)
		}
	}
}

func (m *esModule) walkAccess(n *ast.Node, sc *scope, fn func(ref)) {
	access := n.AsPropertyAccessExpression()
	if access.Expression != nil && access.Expression.Kind == ast.KindIdentifier && access.Name() != nil {
		obj := access.Expression.Text()
		if !sc.shadows(obj) {
			if imp, ok := m.imports[obj]; ok && imp.namespace {
				fn(ref{kind: refNSProp, name: obj, prop: access.Name().Text(), node: n})
				return
			}
		}
	}
	m.walk(access.Expression, n, sc, fn)
}

func (m *esModule) walkElement(n *ast.Node, sc *scope, fn func(ref)) {
	el := n.AsElementAccessExpression()
	if el.Expression != nil && el.Expression.Kind == ast.KindIdentifier && el.ArgumentExpression != nil &&
		el.ArgumentExpression.Kind == ast.KindStringLiteral {
		obj := el.Expression.Text()
		if !sc.shadows(obj) {
			if imp, ok := m.imports[obj]; ok && imp.namespace {
				fn(ref{kind: refNSProp, name: obj, prop: el.ArgumentExpression.Text(), node: n})
				return
			}
		}
	}
	m.walk(el.Expression, n, sc, fn)
	m.walk(el.ArgumentExpression, n, sc, fn)
}

func (m *esModule) noteDynamic(n *ast.Node, fn func(ref)) {
	args := n.Arguments()
	if len(args) != 1 || args[0].Kind != ast.KindStringLiteral {
		call := n.AsCallExpression()
		m.walk(call.Expression, n, m.moduleScope(), fn)
		for _, arg := range args {
			m.walk(arg, n, m.moduleScope(), fn)
		}
		return
	}
	spec := args[0].Text()
	resolved, external, err := m.b.resolve(m.osPath(), spec)
	if err != nil || external {
		fn(ref{kind: refDynamic, name: spec, external: true, node: n})
		return
	}
	fn(ref{kind: refDynamic, name: spec, module: moduleID(resolved), node: n})
}

func (m *esModule) noteIdent(n, parent *ast.Node, sc *scope, fn func(ref)) {
	if isDeclarationName(n, parent) || isPropertyName(n, parent) {
		return
	}
	name := n.Text()
	if sc.shadows(name) {
		return
	}
	if imp, ok := m.imports[name]; ok && imp.namespace {
		fn(ref{kind: refNSValue, name: name, node: n})
		return
	}
	shorthand := parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment
	fn(ref{kind: refName, name: name, node: n, shorthand: shorthand})
}

func isDeclarationName(n, parent *ast.Node) bool {
	if parent == nil || n == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindFunctionDeclaration:
		name := parent.AsFunctionDeclaration().Name()
		return name == n
	case ast.KindFunctionExpression:
		name := parent.AsFunctionExpression().Name()
		return name == n
	case ast.KindClassDeclaration:
		name := parent.AsClassDeclaration().Name()
		return name == n
	case ast.KindVariableDeclaration:
		return parent.AsVariableDeclaration().Name() == n
	case ast.KindParameter:
		return parent.AsParameterDeclaration().Name() == n
	case ast.KindBindingElement:
		return parent.AsBindingElement().Name() == n
	default:
		return false
	}
}

func isPropertyName(n, parent *ast.Node) bool {
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		return parent.AsPropertyAccessExpression().Name() == n
	case ast.KindPropertyAssignment:
		return parent.AsPropertyAssignment().Name() == n
	case ast.KindMethodDeclaration:
		return parent.AsMethodDeclaration().Name() == n
	case ast.KindGetAccessor:
		return parent.AsGetAccessorDeclaration().Name() == n
	case ast.KindSetAccessor:
		return parent.AsSetAccessorDeclaration().Name() == n
	default:
		return false
	}
}

func defineLexical(n *ast.Node, sc *scope) {
	if n == nil || n.Kind != ast.KindVariableStatement {
		return
	}
	listNode := n.AsVariableStatement().DeclarationList
	if listNode == nil {
		return
	}
	list := listNode.AsVariableDeclarationList()
	if list.Flags&(ast.NodeFlagsLet|ast.NodeFlagsConst) == 0 || list.Declarations == nil {
		return
	}
	for _, declNode := range list.Declarations.Nodes {
		eachBindingIdent(declNode.AsVariableDeclaration().Name(), func(id *ast.Node) {
			sc.define(id.Text())
		})
	}
}

func collectVars(n *ast.Node, fnScope *scope) {
	if n == nil {
		return
	}
	switch n.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindClassDeclaration, ast.KindClassExpression:
		return
	case ast.KindVariableStatement:
		listNode := n.AsVariableStatement().DeclarationList
		if listNode == nil {
			return
		}
		list := listNode.AsVariableDeclarationList()
		if list.Declarations == nil {
			return
		}
		lexical := list.Flags&(ast.NodeFlagsLet|ast.NodeFlagsConst) != 0
		for _, declNode := range list.Declarations.Nodes {
			decl := declNode.AsVariableDeclaration()
			if !lexical {
				eachBindingIdent(decl.Name(), func(id *ast.Node) { fnScope.define(id.Text()) })
			}
			collectVars(decl.Initializer, fnScope)
		}
		return
	}
	n.ForEachChild(func(child *ast.Node) bool {
		collectVars(child, fnScope)
		return false
	})
}

func eachBindingIdent(n *ast.Node, fn func(*ast.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case ast.KindIdentifier:
		fn(n)
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		n.ForEachChild(func(child *ast.Node) bool {
			if child.Kind == ast.KindBindingElement {
				eachBindingIdent(child.AsBindingElement().Name(), fn)
			}
			return false
		})
	}
}

func isSkippableStmt(src string, stmt *ast.Node) bool {
	switch stmt.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration:
		return true
	case ast.KindExportAssignment:
		exp := stmt.AsExportAssignment()
		return exp.Expression != nil && exp.Expression.Kind == ast.KindIdentifier
	case ast.KindExpressionStatement:
		expr := stmt.AsExpressionStatement().Expression
		if expr != nil && expr.Kind == ast.KindStringLiteral && expr.Text() == "use strict" {
			return true
		}
		// Pure expression statements (incl. /*#__PURE__*/ calls) have no live binding.
		return isPureExpr(src, expr)
	default:
		return false
	}
}

func assignNames(mods map[string]*esModule, order []string, names *namer) error {
	preferred := map[extKey]string{}
	for _, id := range order {
		m := mods[id]
		for _, imp := range m.imports {
			if !imp.external || !importLive(m, imp) {
				continue
			}
			if imp.namespace && imp.escapes {
				key := extKey{imp.spec, "*"}
				if _, ok := preferred[key]; !ok {
					preferred[key] = imp.local
				}
				continue
			}
			if imp.namespace {
				for prop := range imp.props {
					key := extKey{imp.spec, prop}
					if _, ok := preferred[key]; !ok {
						preferred[key] = prop
					}
				}
				continue
			}
			key := extKey{imp.spec, imp.imported}
			if _, ok := preferred[key]; !ok {
				if imp.imported == "default" {
					preferred[key] = imp.local
				} else {
					preferred[key] = imp.imported
				}
			}
		}
	}
	extFinal := map[extKey]string{}
	keys := make([]extKey, 0, len(preferred))
	for key := range preferred {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b extKey) int {
		if a.spec != b.spec {
			return strings.Compare(a.spec, b.spec)
		}
		return strings.Compare(a.name, b.name)
	})
	for _, key := range keys {
		extFinal[key] = names.take(preferred[key])
	}
	for _, id := range order {
		m := mods[id]
		locals := make([]string, 0, len(m.locals))
		for name, loc := range m.locals {
			if m.liveLoc[name] || (loc.defExpr && m.liveExp["default"]) {
				locals = append(locals, name)
			}
		}
		slices.Sort(locals)
		for _, name := range locals {
			loc := m.locals[name]
			pref := name
			switch {
			case loc.defExpr:
				pref = "_default"
			case loc.cjsFactory:
				pref = cjsFactoryPreferredName(m.id)
			case name == cjsExportsLocal:
				pref = cjsExportsLocal
			}
			loc.final = names.take(pref)
		}
		for _, imp := range m.imports {
			if !imp.external {
				continue
			}
			if imp.namespace && imp.escapes {
				imp.final = extFinal[extKey{imp.spec, "*"}]
				continue
			}
			if !imp.namespace {
				imp.final = extFinal[extKey{imp.spec, imp.imported}]
			}
		}
		for _, imp := range m.imports {
			if imp.external || !imp.namespace || !imp.escapes {
				continue
			}
			imp.final = names.take(imp.local)
		}
	}
	for _, id := range order {
		m := mods[id]
		m.extFinal = extFinal
	}
	for _, id := range order {
		m := mods[id]
		for _, imp := range m.imports {
			if imp.external || imp.namespace || imp.final != "" {
				continue
			}
			dep := mods[imp.module]
			if dep == nil {
				continue
			}
			target, ok, err := dep.resolveExport(mods, imp.imported, map[string]bool{})
			if err != nil || !ok || target.mod == nil {
				continue
			}
			if loc := target.mod.locals[target.local]; loc != nil {
				imp.final = loc.final
			}
		}
	}
	return nil
}

func importLive(m *esModule, imp *esImport) bool {
	if imp.external && imp.namespace && (imp.escapes || len(imp.props) > 0) {
		return true
	}
	if imp.namespace {
		return false
	}
	// A named/default import is live when its local name was referenced from
	// live code. markImport records that by marking the dependency export.
	// External imports have no dependency module, so treat props/escapes and
	// any live local that is this import as used. References set escapes or
	// are discovered by scanning; we mark external imports live if the scan
	// saw them. The scan sets a flag via props for namespaces. For named
	// imports, applyRef calls markImport which for external returns nil
	// without recording. Record usage on the import during applyRef instead.
	return imp.used
}

func (m *esModule) markImport(mods map[string]*esModule, imp *esImport, prop string) error {
	imp.used = true
	if prop != "" && prop != "*" {
		imp.props[prop] = true
	}
	if imp.external {
		return nil
	}
	dep := mods[imp.module]
	if dep == nil {
		return nil
	}
	m.need(dep.id)
	if imp.namespace && (prop == "*" || imp.escapes) {
		names, err := dep.publicNames(mods, map[string]bool{})
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := markExport(mods, dep, name, map[string]bool{}); err != nil {
				return err
			}
		}
		return nil
	}
	imported := imp.imported
	if prop != "" && prop != "*" {
		imported = prop
	}
	if imported == "*" {
		return nil
	}
	return markExport(mods, dep, imported, map[string]bool{})
}

func renderLinked(mods map[string]*esModule, order []string, entry *esModule, format Format, helper string) string {
	var paths []string
	for _, id := range order {
		paths = append(paths, filepath.FromSlash(id))
	}
	root := displayRoot(paths)
	sorted := topoModules(mods, entry)

	var b strings.Builder
	var lines []string
	if format == FormatCJS {
		lines = externalRequires(sorted)
	} else {
		lines = externalImports(sorted)
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(lines) > 0 {
		b.WriteByte('\n')
	}
	if helper != "" && anyCJSDynamic(sorted) {
		b.WriteString(commonJSHelperSource(helper))
		b.WriteString("\n\n")
	}
	for _, m := range sorted {
		body := strings.TrimSpace(m.renderBody(mods, helper))
		if body == "" {
			continue
		}
		fmt.Fprintf(&b, "// %s\n%s\n\n", displayRel(root, m.osPath()), body)
	}
	named, def := entry.exportList(mods)
	if format == FormatCJS {
		b.WriteString("module.exports = {\n")
		for _, item := range named {
			key := item
			binding := item
			if left, right, ok := strings.Cut(item, " as "); ok {
				binding, key = left, right
			}
			if isIdentifierName(key) {
				fmt.Fprintf(&b, "  %s: %s,\n", key, binding)
			} else {
				fmt.Fprintf(&b, "  %s: %s,\n", strconv.Quote(key), binding)
			}
		}
		if def != "" {
			fmt.Fprintf(&b, "  default: %s,\n", def)
		}
		b.WriteString("};\n")
		return b.String()
	}
	if len(named) == 0 && def == "" {
		if !strings.Contains(b.String(), "import ") {
			b.WriteString("export {};\n")
		}
		return b.String()
	}
	if len(named) > 0 {
		b.WriteString("export {\n")
		for _, item := range named {
			b.WriteString("  " + item + ",\n")
		}
		b.WriteString("};\n")
	}
	if def != "" {
		fmt.Fprintf(&b, "export default %s;\n", def)
	}
	return b.String()
}

func externalRequires(mods []*esModule) []string {
	type named struct{ exported, local string }
	type group struct {
		spec  string
		def   string
		ns    string
		named []named
	}
	groups := map[string]*group{}
	var specs []string
	for _, m := range mods {
		for _, imp := range m.imports {
			if !imp.external || !importLive(m, imp) {
				continue
			}
			g := groups[imp.spec]
			if g == nil {
				g = &group{spec: imp.spec}
				groups[imp.spec] = g
				specs = append(specs, imp.spec)
			}
			switch {
			case imp.namespace && imp.escapes:
				g.ns = imp.final
			case imp.namespace:
				for prop := range imp.props {
					g.named = append(g.named, named{prop, m.extFinal[extKey{imp.spec, prop}]})
				}
			case imp.imported == "default":
				g.def = imp.final
			default:
				g.named = append(g.named, named{imp.imported, imp.final})
			}
		}
	}
	slices.Sort(specs)
	var lines []string
	for _, spec := range specs {
		g := groups[spec]
		if g.ns != "" {
			lines = append(lines, fmt.Sprintf("const %s = require(%s);", g.ns, strconv.Quote(g.spec)))
		}
		if g.def != "" {
			lines = append(lines, fmt.Sprintf("const %s = require(%s);", g.def, strconv.Quote(g.spec)))
		}
		seen := map[string]bool{}
		var parts []string
		slices.SortFunc(g.named, func(a, b named) int { return strings.Compare(a.exported, b.exported) })
		for _, item := range g.named {
			if item.local == "" || seen[item.exported] {
				continue
			}
			seen[item.exported] = true
			if item.local == item.exported {
				parts = append(parts, item.exported)
			} else {
				parts = append(parts, item.exported+": "+item.local)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, fmt.Sprintf("const { %s } = require(%s);", strings.Join(parts, ", "), strconv.Quote(g.spec)))
		}
	}
	return lines
}

func topoModules(mods map[string]*esModule, entry *esModule) []*esModule {
	var out []*esModule
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(*esModule)
	visit = func(m *esModule) {
		if m == nil || !m.inst || done[m.id] || visiting[m.id] {
			return
		}
		visiting[m.id] = true
		deps := m.deps(mods)
		slices.Sort(deps)
		for _, id := range deps {
			visit(mods[id])
		}
		visiting[m.id] = false
		done[m.id] = true
		out = append(out, m)
	}
	visit(entry)
	return out
}

func (m *esModule) deps(mods map[string]*esModule) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] || mods[id] == nil || !mods[id].inst {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range m.needed {
		add(id)
	}
	for _, id := range m.sideImps {
		add(id)
	}
	return out
}

func externalImports(mods []*esModule) []string {
	type named struct{ exported, local string }
	type group struct {
		spec  string
		def   string
		ns    string
		named []named
	}
	groups := map[string]*group{}
	var specs []string
	for _, m := range mods {
		for _, imp := range m.imports {
			if !imp.external || !importLive(m, imp) {
				continue
			}
			g := groups[imp.spec]
			if g == nil {
				g = &group{spec: imp.spec}
				groups[imp.spec] = g
				specs = append(specs, imp.spec)
			}
			switch {
			case imp.namespace && imp.escapes:
				g.ns = imp.final
			case imp.namespace:
				for prop := range imp.props {
					g.named = append(g.named, named{prop, m.extFinal[extKey{imp.spec, prop}]})
				}
			case imp.imported == "default":
				g.def = imp.final
			default:
				g.named = append(g.named, named{imp.imported, imp.final})
			}
		}
	}
	slices.Sort(specs)
	var lines []string
	for _, spec := range specs {
		g := groups[spec]
		if g.ns != "" {
			lines = append(lines, fmt.Sprintf("import * as %s from %s;", g.ns, strconv.Quote(g.spec)))
		}
		seen := map[string]bool{}
		var parts []string
		slices.SortFunc(g.named, func(a, b named) int { return strings.Compare(a.exported, b.exported) })
		for _, item := range g.named {
			if item.local == "" || seen[item.exported] {
				continue
			}
			seen[item.exported] = true
			if item.local == item.exported {
				parts = append(parts, item.exported)
			} else {
				parts = append(parts, item.exported+" as "+item.local)
			}
		}
		if g.def == "" && len(parts) == 0 {
			continue
		}
		line := "import "
		if g.def != "" {
			line += g.def
			if len(parts) > 0 {
				line += ", "
			}
		}
		if len(parts) > 0 {
			line += "{ " + strings.Join(parts, ", ") + " }"
		}
		line += " from " + strconv.Quote(g.spec) + ";"
		lines = append(lines, line)
	}
	return lines
}

func (entry *esModule) exportList(mods map[string]*esModule) (named []string, def string) {
	names, err := entry.publicNames(mods, map[string]bool{})
	if err != nil {
		return nil, ""
	}
	for _, name := range names {
		if !entry.liveExp[name] {
			continue
		}
		target, ok, err := entry.resolveExport(mods, name, map[string]bool{})
		if err != nil || !ok {
			continue
		}
		binding := bindingText(target, entry)
		if binding == "" {
			continue
		}
		if name == "default" {
			def = binding
			continue
		}
		if binding == name && isIdentifierName(name) {
			named = append(named, name)
			continue
		}
		if isIdentifierName(name) {
			named = append(named, binding+" as "+name)
			continue
		}
		named = append(named, binding+" as "+strconv.Quote(name))
	}
	return named, def
}

func bindingText(target expTarget, m *esModule) string {
	if target.mod != nil && target.local != "" && target.mod.locals[target.local] != nil {
		return target.mod.locals[target.local].final
	}
	if target.extSpec != "" {
		return m.extFinal[extKey{target.extSpec, target.extName}]
	}
	return ""
}

func (m *esModule) renderBody(mods map[string]*esModule, helper string) string {
	var chunks []string
	for _, imp := range m.namespaceObjects(mods) {
		chunks = append(chunks, imp)
	}
	chunks = append(chunks, m.renderCJSBindings(mods, helper)...)
	if !m.cjsWrapAll && m.file != nil && m.file.Statements != nil {
		var edits []edit
		for _, stmt := range m.file.Statements.Nodes {
			edits = append(edits, m.statementEdits(mods, stmt)...)
		}
		if text, err := applyEdits(m.src, edits); err == nil {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				chunks = append(chunks, trimmed)
			}
		}
	}
	return strings.Join(chunks, "\n")
}

func (m *esModule) namespaceObjects(mods map[string]*esModule) []string {
	var locals []string
	for name := range m.imports {
		locals = append(locals, name)
	}
	slices.Sort(locals)
	var out []string
	for _, name := range locals {
		imp := m.imports[name]
		if imp.external || !imp.namespace || !imp.escapes || imp.final == "" {
			continue
		}
		dep := mods[imp.module]
		if dep == nil {
			continue
		}
		names, _ := dep.publicNames(mods, map[string]bool{})
		var props []string
		for _, exp := range names {
			if !dep.liveExp[exp] {
				continue
			}
			target, ok, err := dep.resolveExport(mods, exp, map[string]bool{})
			if err != nil || !ok {
				continue
			}
			binding := bindingText(target, dep)
			if binding == "" {
				continue
			}
			props = append(props, propLine(exp, binding))
		}
		out = append(out, "const "+imp.final+" = {\n"+strings.Join(props, "\n")+"\n};")
	}
	return out
}

func propLine(name, binding string) string {
	if isIdentifierName(name) {
		return "  get " + name + "() { return " + binding + "; },"
	}
	return "  get " + strconv.Quote(name) + "() { return " + binding + "; },"
}

func (m *esModule) statementEdits(mods map[string]*esModule, stmt *ast.Node) []edit {
	if stmt.Pos() < 0 || stmt.End() <= stmt.Pos() {
		return nil
	}
	if !m.keepStmt(stmt) {
		return []edit{{start: stmt.Pos(), end: stmt.End(), text: ""}}
	}
	if stmt.Kind == ast.KindExportAssignment {
		exp := stmt.AsExportAssignment()
		if exp.Expression == nil || exp.Expression.Kind == ast.KindIdentifier {
			return []edit{{start: stmt.Pos(), end: stmt.End(), text: ""}}
		}
		loc := m.locals["default"]
		if loc == nil || loc.final == "" {
			return []edit{{start: stmt.Pos(), end: stmt.End(), text: ""}}
		}
		expr := m.renameSpan(exp.Expression.Pos(), exp.Expression.End())
		return []edit{{start: stmt.Pos(), end: stmt.End(), text: "const " + loc.final + " = " + expr + ";"}}
	}
	var edits []edit
	if start, end, ok := stripExportSpan(m.src, stmt); ok {
		edits = append(edits, edit{start: start, end: end, text: ""})
	}
	m.walkRefs(stmt, nil, m.moduleScope(), func(r ref) {
		switch r.kind {
		case refName:
			final, ok := m.outputName(r.name)
			if !ok || r.node == nil {
				return
			}
			text := final
			if r.shorthand && final != r.node.Text() {
				text = r.node.Text() + ": " + final
			} else if final == r.node.Text() {
				return
			}
			edits = append(edits, edit{start: tokenStart(m.src, r.node), end: r.node.End(), text: text})
		case refNSProp:
			imp := m.imports[r.name]
			if imp == nil {
				return
			}
			if imp.escapes {
				if imp.final != "" && imp.final != r.name {
					obj := objectIdent(r.node)
					if obj != nil {
						edits = append(edits, edit{start: tokenStart(m.src, obj), end: obj.End(), text: imp.final})
					}
				}
				return
			}
			if final := m.propBinding(mods, imp, r.prop); final != "" {
				edits = append(edits, edit{start: tokenStart(m.src, r.node), end: r.node.End(), text: final})
			}
		case refDynamic:
			if r.external || r.module == "" {
				return
			}
			edits = append(edits, edit{start: tokenStart(m.src, r.node), end: r.node.End(), text: dynamicNamespace(mods, r.module)})
		}
	})
	for _, loc := range m.locals {
		if loc.final == "" || loc.stmt != stmt {
			continue
		}
		for _, id := range loc.idents {
			if id.Text() == loc.final {
				continue
			}
			edits = append(edits, edit{start: tokenStart(m.src, id), end: id.End(), text: loc.final})
		}
	}
	return edits
}

func (m *esModule) keepStmt(stmt *ast.Node) bool {
	if m.cjsWrapAll || m.cjsSkip[stmt] {
		return false
	}
	if isSkippableStmt(m.src, stmt) {
		return false
	}
	if isPureDecl(m.src, stmt) {
		names := declaredLocals(stmt)
		for _, name := range names {
			if m.liveLoc[name] {
				return true
			}
		}
		if len(names) > 0 {
			return false
		}
	}
	return true
}

func declaredLocals(stmt *ast.Node) []string {
	var names []string
	switch stmt.Kind {
	case ast.KindFunctionDeclaration:
		if name := stmt.AsFunctionDeclaration().Name(); name != nil {
			names = append(names, name.Text())
		}
	case ast.KindClassDeclaration:
		if name := stmt.AsClassDeclaration().Name(); name != nil {
			names = append(names, name.Text())
		}
	case ast.KindVariableStatement:
		listNode := stmt.AsVariableStatement().DeclarationList
		if listNode == nil {
			return nil
		}
		list := listNode.AsVariableDeclarationList()
		if list.Declarations == nil {
			return nil
		}
		for _, declNode := range list.Declarations.Nodes {
			eachBindingIdent(declNode.AsVariableDeclaration().Name(), func(id *ast.Node) {
				names = append(names, id.Text())
			})
		}
	}
	return names
}

func (m *esModule) outputName(name string) (string, bool) {
	if imp, ok := m.imports[name]; ok && imp.final != "" {
		return imp.final, true
	}
	if loc, ok := m.locals[name]; ok && loc.final != "" {
		return loc.final, true
	}
	return "", false
}

func (m *esModule) propBinding(mods map[string]*esModule, imp *esImport, prop string) string {
	if imp.external {
		return m.extFinal[extKey{imp.spec, prop}]
	}
	dep := mods[imp.module]
	if dep == nil {
		return ""
	}
	target, ok, err := dep.resolveExport(mods, prop, map[string]bool{})
	if err != nil || !ok {
		return ""
	}
	return bindingText(target, dep)
}

func (m *esModule) renameSpan(start, end int) string {
	var edits []edit
	if m.file != nil {
		m.walkRefs(m.file.AsNode(), nil, m.moduleScope(), func(r ref) {
			if r.node == nil || r.node.Pos() < start || r.node.End() > end || r.kind != refName {
				return
			}
			final, ok := m.outputName(r.name)
			if ok && final != r.node.Text() {
				edits = append(edits, edit{start: r.node.Pos() - start, end: r.node.End() - start, text: final})
			}
		})
	}
	text, err := applyEdits(m.src[start:end], edits)
	if err != nil {
		return m.src[start:end]
	}
	return text
}

func objectIdent(n *ast.Node) *ast.Node {
	switch n.Kind {
	case ast.KindPropertyAccessExpression:
		expr := n.AsPropertyAccessExpression().Expression
		if expr != nil && expr.Kind == ast.KindIdentifier {
			return expr
		}
	case ast.KindElementAccessExpression:
		expr := n.AsElementAccessExpression().Expression
		if expr != nil && expr.Kind == ast.KindIdentifier {
			return expr
		}
	}
	return nil
}

func dynamicNamespace(mods map[string]*esModule, id string) string {
	for _, m := range mods {
		if m != nil && m.b != nil {
			if expr, ok := m.b.splitDynamicImport(id); ok {
				return expr
			}
			break
		}
	}
	dep := mods[id]
	if dep == nil {
		return "Promise.resolve({})"
	}
	names, _ := dep.publicNames(mods, map[string]bool{})
	var parts []string
	for _, name := range names {
		if !dep.liveExp[name] {
			continue
		}
		target, ok, err := dep.resolveExport(mods, name, map[string]bool{})
		if err != nil || !ok {
			continue
		}
		binding := bindingText(target, dep)
		if binding == "" {
			continue
		}
		switch {
		case binding == name && isIdentifierName(name):
			parts = append(parts, name)
		case isIdentifierName(name):
			parts = append(parts, name+": "+binding)
		default:
			parts = append(parts, strconv.Quote(name)+": "+binding)
		}
	}
	return "Promise.resolve({ " + strings.Join(parts, ", ") + " })"
}

func tokenStart(src string, n *ast.Node) int {
	if n == nil || n.Pos() < 0 {
		return 0
	}
	return scanner.SkipTrivia(src, n.Pos())
}

func stripExportSpan(src string, stmt *ast.Node) (int, int, bool) {
	if stmt.Pos() < 0 || stmt.End() > len(src) || stmt.End() < stmt.Pos() {
		return 0, 0, false
	}
	seg := src[stmt.Pos():stmt.End()]
	i := skipTrivia(seg)
	if !hasWord(seg, i, "export") {
		return 0, 0, false
	}
	j := i + len("export")
	j = skipSpaces(seg, j)
	if hasWord(seg, j, "default") {
		j += len("default")
		j = skipSpaces(seg, j)
	}
	return stmt.Pos() + i, stmt.Pos() + j, true
}

func skipTrivia(s string) int {
	i := 0
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r':
			i++
		case strings.HasPrefix(s[i:], "//"):
			if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
				i += nl + 1
				continue
			}
			return len(s)
		case strings.HasPrefix(s[i:], "/*"):
			if end := strings.Index(s[i:], "*/"); end >= 0 {
				i += end + 2
				continue
			}
			return len(s)
		default:
			return i
		}
	}
	return i
}

func skipSpaces(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func hasWord(s string, i int, word string) bool {
	if i < 0 || i+len(word) > len(s) || s[i:i+len(word)] != word {
		return false
	}
	if i+len(word) < len(s) {
		r := rune(s[i+len(word)])
		if r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
