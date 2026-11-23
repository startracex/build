package bundler

import (
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/tspath"
)

func parseTypeScript(src, path string) *ast.SourceFile {
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(path)
	if !ok {
		rooted = tspath.RootedFilePathFromNormalized("/file.ts")
	}
	kind := core.ScriptKindTS
	if modpath.HasExt(path, ".tsx") {
		kind = core.ScriptKindTSX
	}
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: rooted,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(rooted)),
	}, src, kind)
}

func rewriteRelativeSpecifiers(src, fromAbs string, b *bundler, typescript bool) (string, error) {
	var file *ast.SourceFile
	if typescript {
		file = parseTypeScript(src, fromAbs)
	} else {
		file = jsedit.ParseEmittedJS(src)
	}
	if file.Statements == nil {
		return src, nil
	}
	var edits []edit
	for _, stmt := range file.Statements.Nodes {
		for _, specNode := range specifierNodes(stmt) {
			spec := specifierText(specNode)
			if spec == "" {
				continue
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external {
				continue
			}
			if typescript && !b.rewriteDeclarationSpecifier(resolved) {
				continue
			}
			next := b.outputSpecifier(fromAbs, resolved)
			edits = append(edits, edit{
				start: jsedit.TokenStart(src, specNode),
				end:   specNode.End(),
				text:  quoteJS(next),
			})
		}
		jsedit.WalkImportCalls(stmt, func(arg *ast.Node) {
			spec := arg.Text()
			if spec == "" {
				return
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external || (typescript && !b.rewriteDeclarationSpecifier(resolved)) {
				return
			}
			edits = append(edits, edit{
				start: jsedit.TokenStart(src, arg),
				end:   arg.End(),
				text:  quoteJS(b.outputSpecifier(fromAbs, resolved)),
			})
		})
		jsedit.WalkRequires(stmt, func(arg *ast.Node) {
			spec := arg.Text()
			if spec == "" {
				return
			}
			resolved, external, err := b.resolve(fromAbs, spec)
			if err != nil || external || (typescript && !b.rewriteDeclarationSpecifier(resolved)) {
				return
			}
			edits = append(edits, edit{
				start: jsedit.TokenStart(src, arg),
				end:   arg.End(),
				text:  quoteJS(b.outputSpecifier(fromAbs, resolved)),
			})
		})
	}
	if len(edits) == 0 {
		return src, nil
	}
	js := make([]jsedit.Edit, len(edits))
	for i, e := range edits {
		js[i] = jsedit.Edit{Start: e.start, End: e.end, Text: e.text}
	}
	return jsedit.ApplyEdits(src, js)
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

// rewriteDeclarationSpecifier reports whether a declaration reference should be
// rewritten to the emitted file. A package reference keeps its specifier: types
// do not need the bundled copy, and the consumer resolves them from its own
// node_modules. A file that was not emitted has no target to point at either.
func (b *bundler) rewriteDeclarationSpecifier(resolved string) bool {
	if isPackagePath(resolved) {
		return false
	}
	return b.outputPaths[modpath.ModuleID(resolved)] != ""
}

func isPackagePath(abs string) bool {
	slash := filepath.ToSlash(abs)
	return strings.HasPrefix(slash, "node_modules/") || strings.Contains(slash, "/node_modules/")
}

func (b *bundler) outputSpecifier(fromAbs, toAbs string) string {
	if len(b.outputPaths) == 0 {
		return relativeOutputSpecifier(fromAbs, toAbs)
	}
	fromOut := b.outputPaths[modpath.ModuleID(fromAbs)]
	toOut := b.outputPaths[modpath.ModuleID(toAbs)]
	if fromOut == "" || toOut == "" {
		return relativeOutputSpecifier(fromAbs, toAbs)
	}
	rel, err := filepath.Rel(filepath.Dir(fromOut), toOut)
	if err != nil {
		return relativeOutputSpecifier(fromAbs, toAbs)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func relativeOutputSpecifier(fromAbs, toAbs string) string {
	fromOut := modpath.JavascriptOutputAbs(fromAbs)
	toOut := modpath.JavascriptOutputAbs(toAbs)
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
