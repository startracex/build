// Package bundler builds one JavaScript file from an entry point.
//
// TypeScript and TSX are parsed, bound, and transpiled with
// github.com/startracex/tsgo. Imports are resolved with tsgo's bundler
// resolver: relative specifiers, node_modules, and package.json "exports"
// and "main". For ESM output, a package.json "module" field is preferred
// when "exports" is absent. Each module is linked with a small runtime.
// ESM output is one linked module in the style of esbuild --bundle --format=esm:
// local files are inlined, unused side-effect-free exports are dropped, and
// Node built-ins stay as import statements. CommonJS output keeps a small
// runtime and assigns the entry exports to module.exports.
//
// Options.Resolve can return a file for a specifier and that file is bundled.
// It is the hook for aliases.
//
// CSS, asset imports, code splitting, and Rollup plugins are out of scope.
package bundler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/binder"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	tsmodule "github.com/startracex/tsgo/pkg/module"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/transpile"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Format selects the bundle's module format.
type Format string

const (
	// FormatESM emits a JavaScript module with import and export statements.
	// Load it from a .mjs file or a package whose type is "module".
	FormatESM Format = "esm"
	// FormatCJS emits a script. Under Node, the entry exports are module.exports.
	FormatCJS Format = "cjs"
)

// JSX selects the JSX transform applied to .tsx and .jsx files.
type JSX string

const (
	// JSXReact emits React.createElement calls. It is the default because
	// that transform does not import another package.
	JSXReact JSX = "react"
	// JSXAutomatic emits the react/jsx-runtime import used by modern TypeScript.
	// That package is resolved and bundled like any other dependency.
	JSXAutomatic JSX = "react-jsx"
)

// Options configures a bundle.
type Options struct {
	// Entry is the file the bundle starts from.
	Entry string
	// Format selects ESM or CommonJS output. Empty means FormatESM.
	Format Format
	// JSX selects the JSX transform. Empty means JSXReact.
	JSX JSX
	// Resolve, when it reports ok, supplies the file that specifier refers to.
	// Return ok false to keep the built-in resolver. The hook is how aliases
	// and, later, plugins replace resolution.
	Resolve func(importer, specifier string) (resolved string, ok bool, err error)
}

// Bundle reads Entry, follows its imports, and returns one JavaScript program.
func Bundle(ctx context.Context, opts Options) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Entry == "" {
		return "", fmt.Errorf("missing entry file")
	}
	format, err := normalizeFormat(opts.Format)
	if err != nil {
		return "", err
	}
	jsx, err := jsxEmit(opts.JSX)
	if err != nil {
		return "", err
	}
	entry, err := filepath.Abs(opts.Entry)
	if err != nil {
		return "", err
	}
	resolver, err := newResolver(filepath.Dir(entry))
	if err != nil {
		return "", err
	}
	mode := core.ModuleKindESNext
	if format == FormatCJS {
		mode = core.ModuleKindCommonJS
	}
	b := &bundler{
		ctx:       ctx,
		opts:      opts,
		format:    format,
		jsx:       jsx,
		mode:      mode,
		resolver:  resolver,
		externals: map[string]struct{}{},
		loading:   map[string]bool{},
		units:     map[string]*unit{},
	}
	if err := b.load(entry); err != nil {
		return "", err
	}
	if format == FormatESM {
		return linkESM(b, moduleID(entry))
	}
	rel := relativeIDs(unitPaths(b, entry))
	code := map[string]string{}
	for id, unit := range b.units {
		code[rel[id]] = replaceQuotedIDs(unit.code, rel)
	}
	return emitCJS(code, rel[moduleID(entry)]), nil
}

type unit struct {
	code string
	file *ast.SourceFile
	json bool
}

type bundler struct {
	ctx       context.Context
	opts      Options
	format    Format
	jsx       core.JsxEmit
	mode      core.ResolutionMode
	resolver  *tsmodule.DefaultResolver
	externals map[string]struct{}
	loading   map[string]bool
	units     map[string]*unit
}

func (b *bundler) load(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	id := moduleID(path)
	if b.loading[id] {
		return nil
	}
	if _, done := b.units[id]; done {
		return nil
	}
	b.loading[id] = true
	if !supportedSource(path) {
		return fmt.Errorf("unsupported module %s", path)
	}

	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	srcBytes = bytes.TrimPrefix(srcBytes, []byte{0xEF, 0xBB, 0xBF})
	src := string(srcBytes)

	if strings.EqualFold(filepath.Ext(path), ".json") {
		if !json.Valid(srcBytes) {
			return fmt.Errorf("%s: invalid JSON", path)
		}
		if b.format == FormatESM {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if !isIdentifierName(name) {
				name = "data"
			}
			text := "const " + name + " = " + strings.TrimSpace(src) + ";\nexport default " + name + ";\n"
			b.units[id] = &unit{code: text, json: true}
			return nil
		}
		b.units[id] = &unit{code: "module.exports = " + src + ";\n", json: true}
		return nil
	}

	file, err := parseSource(path, src)
	if err != nil {
		return err
	}
	if err := errorFromDiagnostics(path, src, file.Diagnostics()); err != nil {
		return err
	}
	if err := errorFromDiagnostics(path, src, file.JSDiagnostics()); err != nil {
		return err
	}
	binder.BindSourceFile(file)
	if err := errorFromDiagnostics(path, src, file.BindDiagnostics()); err != nil {
		return err
	}

	deps, err := b.depsFromSpecifiers(path, specifiers(file))
	if err != nil {
		return err
	}

	moduleKind := core.ModuleKindCommonJS
	if b.format == FormatESM {
		moduleKind = core.ModuleKindESNext
	}
	out := transpile.TranspileModule(b.ctx, src, transpile.Options{
		FileName: filepath.Base(path),
		CompilerOptions: &core.CompilerOptions{
			Module: moduleKind,
			Target: core.ScriptTargetES2022,
			Jsx:    b.jsx,
		},
		ReportDiagnostics: true,
	})
	if out == nil {
		return fmt.Errorf("%s: transpile canceled", path)
	}
	if err := errorFromDiagnostics(path, src, out.Diagnostics); err != nil {
		return err
	}

	js := stripShebang(out.OutputText)
	if b.format == FormatESM {
		emitted := parseEmittedJS(js)
		if err := errorFromDiagnostics(path, js, emitted.Diagnostics()); err != nil {
			return fmt.Errorf("parse emitted javascript: %w", err)
		}
		extra, err := b.depsFromSpecifiers(path, specifiers(emitted))
		if err != nil {
			return err
		}
		deps = append(deps, extra...)
		b.units[id] = &unit{code: js, file: file}
	} else {
		rewritten, more, err := rewriteRequires(js, path, b.resolve)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		deps = append(deps, more...)
		b.units[id] = &unit{code: rewritten, file: file}
	}

	seen := map[string]bool{}
	for _, dep := range deps {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		if err := b.load(dep); err != nil {
			return err
		}
	}
	return nil
}

func (b *bundler) depsFromSpecifiers(importer string, specs []string) ([]string, error) {
	var deps []string
	seen := map[string]bool{}
	for _, spec := range specs {
		resolved, external, err := b.resolve(importer, spec)
		if err != nil {
			return nil, err
		}
		if external || seen[resolved] {
			continue
		}
		seen[resolved] = true
		deps = append(deps, resolved)
	}
	return deps, nil
}

func parseSource(path, src string) (*ast.SourceFile, error) {
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(path)
	if !ok {
		return nil, fmt.Errorf("invalid path %s", path)
	}
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: rooted,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(rooted)),
	}, src, core.EnsureScriptKindFromFileName(rooted)), nil
}

func normalizeFormat(format Format) (Format, error) {
	switch format {
	case "", FormatESM:
		return FormatESM, nil
	case FormatCJS:
		return FormatCJS, nil
	default:
		return "", fmt.Errorf("unsupported format %q (use %s or %s)", format, FormatESM, FormatCJS)
	}
}

func jsxEmit(mode JSX) (core.JsxEmit, error) {
	switch mode {
	case "", JSXReact:
		return core.JsxEmitReact, nil
	case JSXAutomatic:
		return core.JsxEmitReactJSX, nil
	default:
		return core.JsxEmitNone, fmt.Errorf("unsupported jsx mode %q (use %s or %s)", mode, JSXReact, JSXAutomatic)
	}
}

func stripShebang(js string) string {
	if !strings.HasPrefix(js, "#!") {
		return js
	}
	if i := strings.IndexByte(js, '\n'); i >= 0 {
		return js[i+1:]
	}
	return ""
}

func errorFromDiagnostics(path, src string, diags []*ast.Diagnostic) error {
	var b strings.Builder
	n := 0
	for _, d := range diags {
		if d == nil || d.Category() != diagnostics.CategoryError {
			continue
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		line, col := lineCol(src, d.Pos())
		fmt.Fprintf(&b, "%s:%d:%d: TS%d: %s", path, line, col, d.Code(), d.Localize(locale.Default))
		n++
	}
	if n == 0 {
		return nil
	}
	return fmt.Errorf("%s", b.String())
}

func lineCol(src string, pos int) (int, int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(src) {
		pos = len(src)
	}
	line, col := 1, 1
	for i := 0; i < pos; i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func parseEmittedJS(src string) *ast.SourceFile {
	fileName := tspath.RootedFilePathFromNormalized("/emit.js")
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		PathKey:  tspath.CaseSensitive.PathKey(tspath.RootedPath(fileName)),
	}, src, core.ScriptKindJS)
}

func unitPaths(b *bundler, entry string) []string {
	paths := []string{entry}
	for id := range b.units {
		paths = append(paths, filepath.FromSlash(id))
	}
	return paths
}

func relativeIDs(paths []string) map[string]string {
	root := displayRoot(paths)
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		out[moduleID(p)] = displayRel(root, p)
	}
	return out
}

func displayRoot(paths []string) string {
	cwd, err := os.Getwd()
	if err == nil && len(paths) > 0 {
		inside := true
		for _, p := range paths {
			rel, relErr := filepath.Rel(cwd, p)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				inside = false
				break
			}
		}
		if inside {
			return cwd
		}
	}
	if len(paths) == 0 {
		return cwd
	}
	root := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		root = commonDir(root, p)
	}
	return root
}

func commonDir(dir, path string) string {
	for {
		rel, err := filepath.Rel(dir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

func displayRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.ToSlash(filepath.Base(path))
	}
	return filepath.ToSlash(rel)
}

func replaceQuotedIDs(code string, rel map[string]string) string {
	keys := make([]string, 0, len(rel))
	for abs := range rel {
		keys = append(keys, abs)
	}
	slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
	for _, abs := range keys {
		code = strings.ReplaceAll(code, strconv.Quote(abs), strconv.Quote(rel[abs]))
	}
	return code
}

func emitCJS(modules map[string]string, entry string) string {
	ids := make([]string, 0, len(modules))
	for id := range modules {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var b strings.Builder
	b.WriteString("// Generated by github.com/startracex/build\n")
	b.WriteString("(function () {\n")
	b.WriteString("  \"use strict\";\n")
	b.WriteString("  var __factories = {\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "    %s: function (exports, require, module, __filename, __dirname) {\n", strconv.Quote(id))
		b.WriteString(modules[id])
		if !strings.HasSuffix(modules[id], "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("    },\n")
	}
	b.WriteString("  };\n")
	b.WriteString("  var __cache = Object.create(null);\n")
	b.WriteString("  var __nodeRequire = typeof require === \"function\" ? require : function (id) {\n")
	b.WriteString("    throw new Error(\"Cannot find module '\" + id + \"'\");\n")
	b.WriteString("  };\n")
	b.WriteString("  function __require(id) {\n")
	b.WriteString("    var cached = __cache[id];\n")
	b.WriteString("    if (cached) return cached.exports;\n")
	b.WriteString("    var factory = __factories[id];\n")
	b.WriteString("    if (typeof factory !== \"function\") return __nodeRequire(id);\n")
	b.WriteString("    var module = { exports: {} };\n")
	b.WriteString("    __cache[id] = module;\n")
	b.WriteString("    var slash = id.lastIndexOf(\"/\");\n")
	b.WriteString("    var dir = slash >= 0 ? id.slice(0, slash) : \".\";\n")
	b.WriteString("    factory.call(module.exports, module.exports, __require, module, id, dir);\n")
	b.WriteString("    return module.exports;\n")
	b.WriteString("  }\n")
	fmt.Fprintf(&b, "  var __entry = __require(%s);\n", strconv.Quote(entry))
	b.WriteString("  if (typeof module === \"object\" && module && module.exports) {\n")
	b.WriteString("    module.exports = __entry;\n")
	b.WriteString("  }\n")
	b.WriteString("})();\n")
	return b.String()
}
