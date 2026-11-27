package link

import (
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

// scanObjectProps scans only the used property initializers of an object-literal binding.
func (m *esModule) scanObjectProps(mods map[string]*esModule, loc *esLocal) error {
	if loc == nil || loc.stmt == nil || loc.escapes {
		return m.scanNode(mods, loc.stmt)
	}
	init := objectInitOfLocal(loc)
	if init == nil || init.Kind != ast.KindObjectLiteralExpression {
		return m.scanNode(mods, loc.stmt)
	}
	list := init.AsObjectLiteralExpression().Properties
	if list == nil {
		return nil
	}
	used := objectUsedProps(init, loc.props)
	for _, prop := range list.Nodes {
		key, val := objectPropParts(prop)
		if key == "" || !used[key] {
			continue
		}
		if val != nil {
			if err := m.scanNode(mods, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func objectInitOfLocal(loc *esLocal) *ast.Node {
	if loc == nil || loc.stmt == nil || loc.stmt.Kind != ast.KindVariableStatement {
		return nil
	}
	listNode := loc.stmt.AsVariableStatement().DeclarationList
	if listNode == nil {
		return nil
	}
	list := listNode.AsVariableDeclarationList()
	if list.Declarations == nil {
		return nil
	}
	for _, declNode := range list.Declarations.Nodes {
		decl := declNode.AsVariableDeclaration()
		name := decl.Name()
		if name != nil && name.Kind == ast.KindIdentifier && name.Text() == loc.name {
			return decl.Initializer
		}
	}
	return nil
}

func objectPropParts(prop *ast.Node) (key string, value *ast.Node) {
	switch prop.Kind {
	case ast.KindPropertyAssignment:
		pa := prop.AsPropertyAssignment()
		key = propertyKeyText(pa.Name())
		return key, pa.Initializer
	case ast.KindShorthandPropertyAssignment:
		sh := prop.AsShorthandPropertyAssignment()
		if sh.Name() != nil {
			return sh.Name().Text(), sh.Name()
		}
	case ast.KindMethodDeclaration:
		md := prop.AsMethodDeclaration()
		return propertyKeyText(md.Name()), prop
	}
	return "", nil
}

func propertyKeyText(name *ast.Node) string {
	if name == nil {
		return ""
	}
	switch name.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral:
		return name.Text()
	default:
		return ""
	}
}

// objectUsedProps returns properties that must stay: those callers read plus those its methods read via `this`, which keeps a prop alive even when no caller names it.
func objectUsedProps(init *ast.Node, props map[string]bool) map[string]bool {
	extra := objectThisProps(init)
	if len(extra) == 0 {
		return props
	}
	merged := make(map[string]bool, len(props)+len(extra))
	for key := range props {
		merged[key] = true
	}
	for key := range extra {
		merged[key] = true
	}
	return merged
}

// objectThisProps lists property names an object literal's methods read through `this`.
func objectThisProps(init *ast.Node) map[string]bool {
	names := map[string]bool{}
	if init == nil || init.Kind != ast.KindObjectLiteralExpression {
		return names
	}
	list := init.AsObjectLiteralExpression().Properties
	if list == nil {
		return names
	}
	var walk func(n *ast.Node)
	walk = func(n *ast.Node) {
		if n == nil {
			return
		}
		if n.Kind == ast.KindPropertyAccessExpression {
			access := n.AsPropertyAccessExpression()
			if access.Expression != nil && access.Expression.Kind == ast.KindThisKeyword && access.Name() != nil {
				names[access.Name().Text()] = true
			}
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	for _, prop := range list.Nodes {
		switch prop.Kind {
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			walk(prop)
		}
	}
	return names
}

// objectPropStripEdits removes unused properties from a non-escaping object binding.
func (m *esModule) objectPropStripEdits(stmt *ast.Node) []jsedit.Edit {
	var edits []jsedit.Edit
	for _, loc := range m.locals {
		if loc.stmt != stmt || loc.escapes || len(loc.props) == 0 {
			continue
		}
		init := objectInitOfLocal(loc)
		if init == nil || init.Kind != ast.KindObjectLiteralExpression {
			continue
		}
		list := init.AsObjectLiteralExpression().Properties
		if list == nil {
			continue
		}
		used := objectUsedProps(init, loc.props)
		nodes := list.Nodes
		for i, prop := range nodes {
			key, _ := objectPropParts(prop)
			if key == "" || used[key] {
				continue
			}
			start := prop.Pos()
			end := prop.End()
			// Drop trailing comma after previous prop or leading comma before next.
			if i+1 < len(nodes) {
				// include whitespace/comma up to next prop
				end = nodes[i+1].Pos()
			} else if i > 0 {
				// include comma after previous prop
				prevEnd := nodes[i-1].End()
				start = prevEnd
			}
			edits = append(edits, jsedit.Edit{Start: start, End: end})
		}
	}
	return edits
}
