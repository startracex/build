package bundler

import (
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/define"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/build/pkg/wrap"
	"github.com/startracex/tsgo/pkg/ast"
)

func moduleID(path string) string { return modpath.ModuleID(path) }

func exportConditions(extra []string) []string { return resolve.ExportConditions(extra) }

func displayRoot(paths []string) string { return modpath.DisplayRoot(paths) }

func displayRel(root, path string) string { return modpath.DisplayRel(root, path) }

func javascriptOutputAbs(abs string) string { return modpath.JavascriptOutputAbs(abs) }

func isJavaScriptOutput(path string) bool { return modpath.IsJavaScriptOutput(path) }

func isTypeScriptPath(path string) bool { return modpath.IsTypeScriptPath(path) }

func stripShebang(js string) string { return jsedit.StripShebang(js) }

func stripSourceMappingComment(src string) string {
	return jsedit.StripSourceMappingComment(src)
}

func parseEmittedJS(src string) *ast.SourceFile { return jsedit.ParseEmittedJS(src) }

func isIdentifierName(name string) bool { return jsedit.IsIdentifierName(name) }

func applyDefineDrop(src string, defs map[string]string, drop []string) string {
	return define.ApplyDefineDrop(src, defs, drop)
}

func wrapFormat(js string, format Format, globalName string) string {
	return wrap.WrapFormat(js, string(format), globalName)
}

func appSourceMap(js, file string) string { return sourcemap.AppSourceMap(js, file) }

func retargetSourceMap(mapText, fileName, sourceName, sourceContent string) string {
	return sourcemap.RetargetSourceMap(mapText, fileName, sourceName, sourceContent)
}

func relativizeDeclarationMap(mapText, fileName, root string) string {
	return sourcemap.RelativizeDeclarationMap(mapText, fileName, root)
}

func newTranspileCache(dir string) *cache.TranspileCache {
	return cache.NewTranspileCache(dir)
}

type cacheEntry = cache.Entry

// DeriveOutFile picks an extension for dual-format outfile emission.
func DeriveOutFile(base string, format Format) string {
	return output.DeriveOutFile(base, string(format))
}

// DeriveOutDir nests format under a shared outdir for multi-format lib builds.
func DeriveOutDir(base string, format Format) string {
	return output.DeriveOutDir(base, string(format))
}

func validateOutputPaths(out OutputOptions, mode Mode, entryCount int) error {
	return output.ValidateOutputPaths(string(out.Format), out.OutFile, out.OutDir, string(mode), entryCount)
}

func applyEdits(src string, edits []edit) (string, error) {
	js := make([]jsedit.Edit, len(edits))
	for i, e := range edits {
		js[i] = jsedit.Edit{Start: e.start, End: e.end, Text: e.text}
	}
	return jsedit.ApplyEdits(src, js)
}

func walkRequires(n *ast.Node, fn func(arg *ast.Node)) {
	jsedit.WalkRequires(n, fn)
}

func walkImportCalls(n *ast.Node, fn func(arg *ast.Node)) {
	jsedit.WalkImportCalls(n, fn)
}
