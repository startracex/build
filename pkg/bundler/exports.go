package bundler

import (
	"slices"
	"strconv"
	"unicode"

	"github.com/startracex/tsgo/pkg/ast"
)

// exportSurface is the entry module's public ESM shape.
// wholeDefault means the default export is the CommonJS module.exports
// object (JSON, scripts, and export =). Otherwise a default export is
// exports.default from TypeScript's ESM-to-CommonJS transform.
type exportSurface struct {
	wholeDefault bool
	hasDefault   bool
	names        []string
}

func (b *bundler) entrySurface(entry string) (exportSurface, error) {
	return b.surfaceOf(entry, map[string]bool{})
}

func (b *bundler) surfaceOf(path string, stack map[string]bool) (exportSurface, error) {
	id := moduleID(path)
	if stack[id] {
		return exportSurface{}, nil
	}
	stack[id] = true
	defer delete(stack, id)

	unit := b.units[id]
	if unit == nil {
		return exportSurface{}, nil
	}
	if unit.json || unit.file == nil || !ast.IsExternalModule(unit.file) {
		return exportSurface{wholeDefault: true, hasDefault: true}, nil
	}

	var surface exportSurface
	names := map[string]struct{}{}
	add := func(name string) {
		if name == "" || name == "default" {
			return
		}
		names[name] = struct{}{}
	}
	if unit.file.Statements == nil {
		return surface, nil
	}
	for _, stmt := range unit.file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindExportAssignment:
			assignment := stmt.AsExportAssignment()
			surface.hasDefault = true
			if assignment.IsExportEquals {
				surface.wholeDefault = true
			}
		case ast.KindExportDeclaration:
			decl := stmt.AsExportDeclaration()
			if decl.IsTypeOnly {
				continue
			}
			if err := b.addExportDeclaration(path, decl, &surface, names, add, stack); err != nil {
				return exportSurface{}, err
			}
		case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindImportEqualsDeclaration:
			continue
		case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			continue
		default:
			if !ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				continue
			}
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsDefault) {
				surface.hasDefault = true
				continue
			}
			addDeclaredNames(stmt, add)
		}
	}
	surface.names = make([]string, 0, len(names))
	for name := range names {
		surface.names = append(surface.names, name)
	}
	slices.Sort(surface.names)
	return surface, nil
}

func (b *bundler) addExportDeclaration(importer string, decl *ast.ExportDeclaration, surface *exportSurface, names map[string]struct{}, add func(string), stack map[string]bool) error {
	if decl.ExportClause == nil {
		if decl.ModuleSpecifier == nil {
			return nil
		}
		spec := specifierText(decl.ModuleSpecifier)
		resolved, external, err := b.resolve(importer, spec)
		if err != nil {
			return err
		}
		if external {
			return errExternalStar(spec)
		}
		nested, err := b.surfaceOf(resolved, stack)
		if err != nil {
			return err
		}
		for _, name := range nested.names {
			add(name)
		}
		return nil
	}
	switch decl.ExportClause.Kind {
	case ast.KindNamespaceExport:
		add(decl.ExportClause.AsNamespaceExport().Name().Text())
	case ast.KindNamedExports:
		elements := decl.ExportClause.AsNamedExports().Elements
		if elements == nil {
			return nil
		}
		for _, el := range elements.Nodes {
			spec := el.AsExportSpecifier()
			if spec.IsTypeOnly || spec.Name() == nil {
				continue
			}
			name := spec.Name().Text()
			if name == "default" {
				surface.hasDefault = true
				continue
			}
			add(name)
		}
	}
	return nil
}

func errExternalStar(spec string) error {
	return &externalStarError{spec: spec}
}

type externalStarError struct{ spec string }

func (e *externalStarError) Error() string {
	return "cannot re-export * from external module " + strconv.Quote(e.spec)
}

func addDeclaredNames(stmt *ast.Node, add func(string)) {
	switch stmt.Kind {
	case ast.KindFunctionDeclaration:
		if name := stmt.AsFunctionDeclaration().Name(); name != nil {
			add(name.Text())
		}
	case ast.KindClassDeclaration:
		if name := stmt.AsClassDeclaration().Name(); name != nil {
			add(name.Text())
		}
	case ast.KindEnumDeclaration:
		if name := stmt.AsEnumDeclaration().Name(); name != nil {
			add(name.Text())
		}
	case ast.KindVariableStatement:
		listNode := stmt.AsVariableStatement().DeclarationList
		if listNode == nil {
			return
		}
		list := listNode.AsVariableDeclarationList()
		if list.Declarations == nil {
			return
		}
		for _, declNode := range list.Declarations.Nodes {
			addBinding(declNode.AsVariableDeclaration().Name(), add)
		}
	}
}

func addBinding(n *ast.Node, add func(string)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case ast.KindIdentifier:
		add(n.Text())
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		n.ForEachChild(func(child *ast.Node) bool {
			if child.Kind == ast.KindBindingElement {
				addBinding(child.AsBindingElement().Name(), add)
			}
			return false
		})
	}
}

func isIdentifierName(name string) bool {
	if name == "" || isReserved(name) {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '$' && r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '$' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isReserved(name string) bool {
	switch name {
	case "break", "case", "catch", "class", "const", "continue", "debugger", "default",
		"delete", "do", "else", "enum", "export", "extends", "false", "finally", "for",
		"function", "if", "import", "in", "instanceof", "new", "null", "return", "super",
		"switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with",
		"yield", "let", "static", "implements", "interface", "package", "private",
		"protected", "public", "await":
		return true
	default:
		return false
	}
}

func freshName(base string, taken map[string]bool) string {
	name := base
	for taken[name] {
		name += "_"
	}
	taken[name] = true
	return name
}

func takenNames(surface exportSurface) map[string]bool {
	taken := map[string]bool{}
	for _, name := range surface.names {
		if isIdentifierName(name) {
			taken[name] = true
		}
	}
	return taken
}
