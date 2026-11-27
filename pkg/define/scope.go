package define

import (
	"github.com/startracex/tsgo/pkg/ast"
)

// scopeNames returns the bindings a node introduces, or nil when it opens no scope, so defines can skip shadowed identifiers.
func scopeNames(src string, n *ast.Node) map[string]bool {
	var names map[string]bool
	add := func(name *ast.Node) {
		if name == nil {
			return
		}
		if names == nil {
			names = map[string]bool{}
		}
		addBindingNames(src, name, names)
	}
	if ast.IsFunctionLike(n) {
		if params := n.ParameterList(); params != nil && params.Nodes != nil {
			for _, param := range params.Nodes {
				add(param.Name())
			}
		}
	}
	switch n.Kind {
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindEnumDeclaration,
		ast.KindModuleDeclaration, ast.KindTypeAliasDeclaration, ast.KindInterfaceDeclaration,
		ast.KindFunctionExpression, ast.KindClassExpression:
		add(n.Name())
	case ast.KindBlock, ast.KindModuleBlock, ast.KindCaseClause, ast.KindDefaultClause:
		for _, stmt := range n.Statements() {
			addStatementNames(src, stmt, add)
		}
	case ast.KindForStatement:
		addDeclarationListNames(src, n.AsForStatement().Initializer, add)
	case ast.KindForInStatement, ast.KindForOfStatement:
		addDeclarationListNames(src, n.AsForInOrOfStatement().Initializer, add)
	case ast.KindCatchClause:
		if decl := n.AsCatchClause().VariableDeclaration; decl != nil {
			add(decl.Name())
		}
	}
	return names
}

func statementScope(src string, statements []*ast.Node) map[string]bool {
	names := map[string]bool{}
	for _, stmt := range statements {
		addStatementNames(src, stmt, func(name *ast.Node) { addBindingNames(src, name, names) })
	}
	return names
}

func addStatementNames(src string, stmt *ast.Node, add func(*ast.Node)) {
	switch stmt.Kind {
	case ast.KindVariableStatement:
		addDeclarationListNames(src, stmt.AsVariableStatement().DeclarationList, add)
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindEnumDeclaration,
		ast.KindModuleDeclaration, ast.KindTypeAliasDeclaration, ast.KindInterfaceDeclaration,
		ast.KindImportEqualsDeclaration:
		add(stmt.Name())
	case ast.KindImportDeclaration:
		clause := stmt.AsImportDeclaration().ImportClause
		if clause == nil || clause.Kind != ast.KindImportClause {
			return
		}
		importClause := clause.AsImportClause()
		add(importClause.Name())
		bindings := importClause.NamedBindings
		if bindings == nil {
			return
		}
		switch bindings.Kind {
		case ast.KindNamespaceImport:
			add(bindings.Name())
		case ast.KindNamedImports:
			elements := bindings.AsNamedImports().Elements
			if elements == nil {
				return
			}
			for _, element := range elements.Nodes {
				add(element.Name())
			}
		}
	}
}

func addDeclarationListNames(src string, node *ast.Node, add func(*ast.Node)) {
	if node == nil || node.Kind != ast.KindVariableDeclarationList {
		return
	}
	declarations := node.AsVariableDeclarationList().Declarations
	if declarations == nil {
		return
	}
	for _, declaration := range declarations.Nodes {
		if declaration.Kind != ast.KindVariableDeclaration {
			continue
		}
		add(declaration.Name())
	}
}

func addBindingNames(src string, name *ast.Node, names map[string]bool) {
	if name == nil {
		return
	}
	switch name.Kind {
	case ast.KindIdentifier:
		names[nodeText(src, name)] = true
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		for _, element := range ast.GetElementsOfBindingOrAssignmentPattern(name) {
			if element == nil {
				continue
			}
			if element.Kind == ast.KindBindingElement {
				addBindingNames(src, element.Name(), names)
				continue
			}
			addBindingNames(src, element, names)
		}
	}
}
