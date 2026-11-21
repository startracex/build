// Package bundler builds one JavaScript file from an ESM entry point.
//
// TypeScript and TSX are parsed, bound, and transpiled with
// github.com/startracex/tsgo. Relative imports form a module graph. Each
// module is emitted with TypeScript's CommonJS transform and linked by a
// small runtime, so default and named exports keep the shape that transform
// produces. The result is a single script. When Node loads it, the entry
// module's exports are assigned to module.exports.
//
// Bare specifiers stay as external require() calls. Options.Resolve can
// return a file for a specifier and that file is bundled. The hook is the
// extension point for package entry points and later plugins.
//
// Chunk splitting, treeshaking, and CSS are out of scope.
package bundler

import (
	"bytes"
	"context"
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
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/transpile"
	"github.com/startracex/tsgo/pkg/tspath"
)

// JSX selects the JSX transform applied to .tsx and .jsx files.
type JSX string

const (
	// JSXReact emits React.createElement calls. It is the default because
	// that transform does not import another package.
	JSXReact JSX = "react"
	// JSXAutomatic emits the react/jsx-runtime import used by modern TypeScript.
	JSXAutomatic JSX = "react-jsx"
)

// Options configures a bundle.
type Options struct {
	// Entry is the file the bundle starts from.
	Entry string
	// JSX selects the JSX transform. Empty means JSXReact.
	JSX JSX
	// Resolve, when it reports ok, supplies the file that specifier refers to.
	// Return ok false to keep the built-in relative resolver. Bare specifiers
	// that the hook does not claim stay external.
	Resolve func(importer, specifier string) (resolved string, ok bool, err error)
}

// Bundle reads Entry, follows its relative imports, and returns one JavaScript program.
func Bundle(ctx context.Context, opts Options) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Entry == "" {
		return "", fmt.Errorf("missing entry file")
	}
	jsx, err := jsxEmit(opts.JSX)
	if err != nil {
		return "", err
	}
	entry, err := filepath.Abs(opts.Entry)
	if err != nil {
		return "", err
	}
	b := &bundler{
		ctx:     ctx,
		opts:    opts,
		jsx:     jsx,
		loading: map[string]bool{},
		code:    map[string]string{},
	}
	if err := b.load(entry); err != nil {
		return "", err
	}
	return emit(b.code, moduleID(entry)), nil
}

type bundler struct {
	ctx     context.Context
	opts    Options
	jsx     core.JsxEmit
	loading map[string]bool
	code    map[string]string
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
	if _, done := b.code[id]; done {
		return nil
	}
	b.loading[id] = true

	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	srcBytes = bytes.TrimPrefix(srcBytes, []byte{0xEF, 0xBB, 0xBF})
	src := string(srcBytes)

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

	out := transpile.TranspileModule(b.ctx, src, transpile.Options{
		FileName: filepath.Base(path),
		CompilerOptions: &core.CompilerOptions{
			Module: core.ModuleKindCommonJS,
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
	rewritten, more, err := rewriteRequires(js, path, b.resolve)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	b.code[id] = rewritten

	seen := map[string]bool{}
	for _, dep := range append(deps, more...) {
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

func emit(modules map[string]string, entry string) string {
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
