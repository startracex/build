package bundler

import (
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
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
	if strings.HasSuffix(strings.ToLower(path), ".tsx") {
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
				start: jsedit.TokenStart(src, specNode),
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
				start: jsedit.TokenStart(src, arg),
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
				start: jsedit.TokenStart(src, arg),
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
