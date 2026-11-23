package bundler

import (
	"github.com/startracex/tsgo/pkg/ast"
)

// specifiers lists module names that must be loaded for value imports,
// re-exports, dynamic import(), and require() calls. Type-only imports are
// omitted because they are erased and do not exist at runtime.
func specifiers(file *ast.SourceFile) []string {
	if file == nil || file.Statements == nil {
		return nil
	}
	var specs []string
	add := func(spec string) {
		if spec != "" {
			specs = append(specs, spec)
		}
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
			decl := stmt.AsImportDeclaration()
			if importLoadsModule(decl) {
				add(specifierText(decl.ModuleSpecifier))
			}
		case ast.KindExportDeclaration:
			decl := stmt.AsExportDeclaration()
			if exportLoadsModule(decl) {
				add(specifierText(decl.ModuleSpecifier))
			}
		case ast.KindImportEqualsDeclaration:
			decl := stmt.AsImportEqualsDeclaration()
			if decl.IsTypeOnly {
				continue
			}
			if name := ast.GetExternalModuleName(stmt); name != nil {
				add(name.Text())
			}
		}
	}
	ast.ForEachDynamicImportOrRequireCall(file, false, true, func(_ *ast.Node, argument *ast.Expression) bool {
		if argument != nil {
			add(argument.Text())
		}
		return false
	})
	return specs
}

func importLoadsModule(decl *ast.ImportDeclaration) bool {
	if decl.ImportClause == nil {
		return true
	}
	clause := decl.ImportClause.AsImportClause()
	if clause.PhaseModifier == ast.KindTypeKeyword {
		return false
	}
	if clause.Name() != nil {
		return true
	}
	bindings := clause.NamedBindings
	if bindings == nil {
		return false
	}
	switch bindings.Kind {
	case ast.KindNamespaceImport:
		return true
	case ast.KindNamedImports:
		elements := bindings.AsNamedImports().Elements
		if elements == nil {
			return false
		}
		for _, el := range elements.Nodes {
			if !el.AsImportSpecifier().IsTypeOnly {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func exportLoadsModule(decl *ast.ExportDeclaration) bool {
	if decl.ModuleSpecifier == nil || decl.IsTypeOnly {
		return false
	}
	clause := decl.ExportClause
	if clause == nil {
		return true
	}
	switch clause.Kind {
	case ast.KindNamespaceExport:
		return true
	case ast.KindNamedExports:
		elements := clause.AsNamedExports().Elements
		if elements == nil {
			return true
		}
		value := false
		for _, el := range elements.Nodes {
			if !el.AsExportSpecifier().IsTypeOnly {
				value = true
				break
			}
		}
		return value
	default:
		return true
	}
}

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
