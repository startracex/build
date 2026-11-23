package wrap

import (
	"fmt"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

const (
	FormatESM  = "esm"
	FormatCJS  = "cjs"
	FormatIIFE = "iife"
	FormatUMD  = "umd"
)

// WrapFormat converts a CJS-style linked body (module.exports) into IIFE or UMD.
// ESM and CJS pass through unchanged.
func WrapFormat(js string, format string, globalName string) string {
	switch format {
	case FormatIIFE:
		return wrapIIFE(js, globalName)
	case FormatUMD:
		return wrapUMD(js, globalName)
	default:
		return js
	}
}

// BodyIndentCols is the column indent applied to the linked body inside a wrap.
func BodyIndentCols(format string) int {
	switch format {
	case FormatIIFE, FormatUMD:
		return 2
	default:
		return 0
	}
}

func wrapIIFE(js string, globalName string) string {
	name := globalName
	if name == "" {
		name = "exports"
	}
	body := ensureCJSBody(strings.TrimSpace(js))
	return fmt.Sprintf(
		"var %s = (function () {\n  var exports = {};\n  var module = { exports: exports };\n%s\n  return module.exports;\n})();\n",
		name,
		indentBlock(body, "  "),
	)
}

func wrapUMD(js string, globalName string) string {
	name := globalName
	if name == "" {
		name = "bundle"
	}
	body := ensureCJSBody(strings.TrimSpace(js))
	return fmt.Sprintf(`(function (root, factory) {
  if (typeof define === "function" && define.amd) {
    define([], factory);
  } else if (typeof module === "object" && module.exports) {
    module.exports = factory();
  } else {
    root[%q] = factory();
  }
})(typeof self !== "undefined" ? self : this, function () {
  var exports = {};
  var module = { exports: exports };
%s
  return module.exports;
});
`, name, indentBlock(body, "  "))
}

func ensureCJSBody(src string) string {
	if strings.Contains(src, "module.exports") {
		return src
	}
	return stripESMExportsToAssignments(src)
}

func indentBlock(src, pad string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

func stripESMExportsToAssignments(src string) string {
	file := jsedit.ParseEmittedJS(src)
	if file.Statements == nil {
		return stripESMExportsLines(src)
	}
	var edits []jsedit.Edit
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindExportAssignment:
			ea := stmt.AsExportAssignment()
			if ea.Expression == nil {
				edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
				continue
			}
			text := "exports.default = " + src[ea.Expression.Pos():ea.Expression.End()] + ";"
			edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: text})
		case ast.KindExportDeclaration:
			ed := stmt.AsExportDeclaration()
			if ed.ModuleSpecifier != nil {
				// Re-export from another module: drop (linker already inlined).
				edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
				continue
			}
			if ed.IsTypeOnly {
				edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
				continue
			}
			if ed.ExportClause == nil {
				edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
				continue
			}
			clause := ed.ExportClause.AsNamedExports()
			if clause.Elements == nil {
				edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
				continue
			}
			var parts []string
			for _, el := range clause.Elements.Nodes {
				spec := el.AsExportSpecifier()
				local := spec.PropertyName
				if local == nil {
					local = spec.Name()
				}
				exported := spec.Name()
				if local == nil || exported == nil {
					continue
				}
				key := exported.Text()
				binding := local.Text()
				if jsedit.IsIdentifierName(key) {
					parts = append(parts, fmt.Sprintf("exports.%s = %s;", key, binding))
				} else {
					parts = append(parts, fmt.Sprintf("exports[%q] = %s;", key, binding))
				}
			}
			edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: strings.Join(parts, "\n")})
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindVariableStatement:
			if !ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				continue
			}
			mods := stmt.Modifiers()
			if mods == nil {
				continue
			}
			var modStart, modEnd int
			found := false
			for _, mod := range mods.Nodes {
				if mod.Kind == ast.KindExportKeyword {
					modStart, modEnd = mod.Pos(), mod.End()
					found = true
					break
				}
			}
			if !found {
				continue
			}
			// Strip `export` keyword; assign to exports after declaration when named.
			edits = append(edits, jsedit.Edit{Start: modStart, End: modEnd, Text: ""})
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsDefault) {
				continue
			}
			name := exportedDeclName(stmt)
			if name == "" {
				continue
			}
			assign := fmt.Sprintf("\nexports.%s = %s;", name, name)
			edits = append(edits, jsedit.Edit{Start: stmt.End(), End: stmt.End(), Text: assign})
		}
	}
	if len(edits) == 0 {
		return stripESMExportsLines(src)
	}
	out, err := jsedit.ApplyEdits(src, edits)
	if err != nil {
		return stripESMExportsLines(src)
	}
	return out
}

func exportedDeclName(stmt *ast.Node) string {
	switch stmt.Kind {
	case ast.KindFunctionDeclaration:
		if n := stmt.AsFunctionDeclaration().Name(); n != nil {
			return n.Text()
		}
	case ast.KindClassDeclaration:
		if n := stmt.AsClassDeclaration().Name(); n != nil {
			return n.Text()
		}
	case ast.KindVariableStatement:
		listNode := stmt.AsVariableStatement().DeclarationList
		if listNode == nil {
			return ""
		}
		list := listNode.AsVariableDeclarationList()
		if list.Declarations == nil || len(list.Declarations.Nodes) != 1 {
			return ""
		}
		name := list.Declarations.Nodes[0].AsVariableDeclaration().Name()
		if name != nil && name.Kind == ast.KindIdentifier {
			return name.Text()
		}
	}
	return ""
}

func stripESMExportsLines(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	inExport := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "export {") {
			inExport = true
			continue
		}
		if inExport {
			if strings.Contains(trim, "}") {
				inExport = false
			}
			continue
		}
		if rest, ok := strings.CutPrefix(trim, "export default "); ok {
			out = append(out, "exports.default = "+rest)
			continue
		}
		if strings.HasPrefix(trim, "export ") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
