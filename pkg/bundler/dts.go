package bundler

import (
	"path/filepath"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/tspath"
)

func assembleAppDTS(b *bundler, entry string) (string, error) {
	var out strings.Builder
	seen := map[string]bool{}
	var walk func(abs string, public bool) error
	walk = func(abs string, public bool) error {
		id := moduleID(abs)
		if seen[id] {
			return nil
		}
		unit := b.units[id]
		if unit == nil || strings.TrimSpace(unit.dts) == "" {
			seen[id] = true
			return nil
		}
		seen[id] = true
		file := parseTypeScript(unit.dts, abs)
		inlined := map[string]bool{}
		if file.Statements != nil {
			for _, stmt := range file.Statements.Nodes {
				for _, spec := range statementSpecifiers(stmt) {
					resolved, external, err := b.resolve(abs, spec)
					if err != nil || external {
						continue
					}
					if err := walk(resolved, false); err != nil {
						return err
					}
					if dep := b.units[moduleID(resolved)]; dep != nil && strings.TrimSpace(dep.dts) != "" {
						inlined[spec] = true
					}
				}
			}
		}
		out.WriteString(renderDTS(unit.dts, file, public, inlined))
		out.WriteByte('\n')
		return nil
	}
	if err := walk(entry, true); err != nil {
		return "", err
	}
	return out.String(), nil
}

func parseTypeScript(src, path string) *ast.SourceFile {
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(path)
	if !ok {
		rooted = tspath.RootedFilePathFromNormalized("/file.ts")
	}
	kind := core.ScriptKindTS
	if strings.HasSuffix(strings.ToLower(path), ".tsx") {
		kind = core.ScriptKindTSX
	}
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: rooted,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(rooted)),
	}, src, kind)
}

func statementSpecifiers(stmt *ast.Node) []string {
	switch stmt.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		return []string{specifierText(stmt.AsImportDeclaration().ModuleSpecifier)}
	case ast.KindExportDeclaration:
		return []string{specifierText(stmt.AsExportDeclaration().ModuleSpecifier)}
	default:
		return nil
	}
}

func renderDTS(src string, file *ast.SourceFile, public bool, inlined map[string]bool) string {
	if file == nil || file.Statements == nil {
		return ""
	}
	var b strings.Builder
	for _, stmt := range file.Statements.Nodes {
		if stmt.Pos() < 0 || stmt.End() > len(src) || stmt.End() <= stmt.Pos() {
			continue
		}
		switch stmt.Kind {
		case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
			spec := specifierText(stmt.AsImportDeclaration().ModuleSpecifier)
			if public && (isExternalDTSImport(stmt) || !inlined[spec]) {
				b.WriteString(strings.TrimSpace(src[tokenStart(src, stmt):stmt.End()]))
				b.WriteByte('\n')
			}
		case ast.KindExportDeclaration:
			decl := stmt.AsExportDeclaration()
			if decl.ModuleSpecifier != nil {
				if public {
					text := exportWithoutFrom(src, stmt)
					if text != "" {
						b.WriteString(text)
						b.WriteByte('\n')
					}
				}
				continue
			}
			if public {
				b.WriteString(strings.TrimSpace(src[stmt.Pos():stmt.End()]))
				b.WriteByte('\n')
			}
		case ast.KindExportAssignment:
			if !public {
				continue
			}
			b.WriteString(strings.TrimSpace(src[stmt.Pos():stmt.End()]))
			b.WriteByte('\n')
		default:
			chunk := strings.TrimSpace(src[tokenStart(src, stmt):stmt.End()])
			if !public {
				chunk = stripLeadingExport(chunk)
			}
			if chunk == "" {
				continue
			}
			b.WriteString(chunk)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func isExternalDTSImport(stmt *ast.Node) bool {
	spec := specifierText(stmt.AsImportDeclaration().ModuleSpecifier)
	return spec != "" && !strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/")
}

func exportWithoutFrom(src string, stmt *ast.Node) string {
	decl := stmt.AsExportDeclaration()
	if decl.ExportClause == nil {
		return ""
	}
	text := strings.TrimSpace(src[tokenStart(src, stmt):stmt.End()])
	if i := strings.Index(text, " from "); i >= 0 {
		text = strings.TrimSpace(text[:i])
		if !strings.HasSuffix(text, ";") {
			text += ";"
		}
	}
	return text
}

func stripLeadingExport(text string) string {
	trim := strings.TrimLeft(text, " \t\r\n")
	if strings.HasPrefix(trim, "export default ") {
		rest := strings.TrimPrefix(trim, "export default ")
		if strings.HasPrefix(rest, "function ") || strings.HasPrefix(rest, "class ") || strings.HasPrefix(rest, "abstract ") {
			return rest
		}
		return ""
	}
	if strings.HasPrefix(trim, "export ") {
		return strings.TrimPrefix(trim, "export ")
	}
	return text
}

func rewriteRelativeSpecifiers(src, fromAbs string, b *bundler, typescript bool) (string, error) {
	var file *ast.SourceFile
	if typescript {
		file = parseTypeScript(src, fromAbs)
	} else {
		file = parseEmittedJS(src)
	}
	if file.Statements == nil {
		return src, nil
	}
	var edits []edit
	for _, stmt := range file.Statements.Nodes {
		for _, specNode := range specifierNodes(stmt) {
			spec := specifierText(specNode)
			if spec == "" || (!strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/")) {
				continue
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external {
				continue
			}
			next := relativeOutputSpecifier(fromAbs, resolved)
			edits = append(edits, edit{
				start: tokenStart(src, specNode),
				end:   specNode.End(),
				text:  quoteJS(next),
			})
		}
		walkImportCalls(stmt, func(arg *ast.Node) {
			spec := arg.Text()
			if spec == "" || (!strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/")) {
				return
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external {
				return
			}
			edits = append(edits, edit{
				start: tokenStart(src, arg),
				end:   arg.End(),
				text:  quoteJS(relativeOutputSpecifier(fromAbs, resolved)),
			})
		})
		walkRequires(stmt, func(arg *ast.Node) {
			spec := arg.Text()
			if spec == "" || (!strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "/")) {
				return
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external {
				return
			}
			edits = append(edits, edit{
				start: tokenStart(src, arg),
				end:   arg.End(),
				text:  quoteJS(relativeOutputSpecifier(fromAbs, resolved)),
			})
		})
	}
	if len(edits) == 0 {
		return src, nil
	}
	return applyEdits(src, edits)
}

func specifierNodes(stmt *ast.Node) []*ast.Node {
	switch stmt.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		spec := stmt.AsImportDeclaration().ModuleSpecifier
		if spec == nil {
			return nil
		}
		return []*ast.Node{spec}
	case ast.KindExportDeclaration:
		spec := stmt.AsExportDeclaration().ModuleSpecifier
		if spec == nil {
			return nil
		}
		return []*ast.Node{spec}
	default:
		return nil
	}
}

func relativeOutputSpecifier(fromAbs, toAbs string) string {
	fromOut := javascriptOutputAbs(fromAbs)
	toOut := javascriptOutputAbs(toAbs)
	rel, err := filepath.Rel(filepath.Dir(fromOut), toOut)
	if err != nil {
		rel = filepath.Base(toOut)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func quoteJS(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
