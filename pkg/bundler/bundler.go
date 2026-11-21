// Package bundler builds a JavaScript or TypeScript program.
//
// TypeScript and TSX are parsed, bound, and transpiled with
// github.com/startracex/tsgo. Imports are resolved with tsgo's bundler
// resolver: relative specifiers, node_modules, and package.json "exports"
// and "main". For ESM output, a package.json "module" field is preferred
// when "exports" is absent.
//
// App mode (the default) links the local graph into one file. ESM output
// uses real import and export declarations. CommonJS output uses the same
// link and assigns the entry exports to module.exports. Unused side-effect-free
// exports are dropped. Node built-ins and packages listed in Options.External
// stay as import or require calls.
//
// Library mode writes one JavaScript file per local module and leaves bare
// package imports external. Relative specifiers point at the emitted files.
//
// Source maps, declarations, and declaration maps follow TypeScript: off unless
// set. Library mode can write a .d.ts and .d.ts.map for each file, using tsgo's
// type checker. App mode does not write declarations. Options.Resolve can
// replace a specifier with a file.
//
// Multiple Outputs emit the same input in several formats (for example ESM and
// CJS). Define/Drop, Alias, asset queries (?raw/?url), CSS, code splitting, and
// a transpile cache are available through Options.
package bundler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/binder"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/transpile"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Mode selects how output files are laid out.
type Mode string

const (
	// ModeApp writes one JavaScript file for the whole program.
	ModeApp Mode = "app"
	// ModeLib writes one JavaScript file per local module, and leaves bare
	// package imports external. This is the multi-file library layout.
	ModeLib Mode = "lib"
)

// Format selects the bundle's module format.
type Format string

const (
	// FormatESM emits a JavaScript module with import and export statements.
	// Load it from a .mjs file or a package whose type is "module".
	FormatESM Format = "esm"
	// FormatCJS emits a script. Under Node, the entry exports are module.exports.
	FormatCJS Format = "cjs"
	// FormatIIFE wraps the bundle in an immediately-invoked function.
	FormatIIFE Format = "iife"
	// FormatUMD wraps the bundle in a Universal Module Definition.
	FormatUMD Format = "umd"
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
	// Entry is the file the bundle starts from. Entries lists more roots.
	// A single-file build allows only one of them.
	Entry string
	// Entries are additional root files. Library output emits every root and
	// the modules each one imports.
	Entries []string
	// Format selects ESM or CommonJS output. Empty means FormatESM.
	Format Format
	// JSX selects the JSX transform. Empty means JSXReact.
	JSX JSX
	// Mode selects app (one file) or lib (one file per module). Empty means app.
	Mode Mode
	// OutFile is the JavaScript path used by app mode. The declaration and
	// source map are written beside it.
	OutFile string
	// OutDir is the directory used by per-module output.
	OutDir string
	// RootDir is the base for paths written under OutDir. Empty uses the
	// common directory of the loaded files.
	RootDir string
	// Declaration writes .d.ts files in library mode. Nil means no, matching tsc.
	// App mode does not write declarations.
	Declaration *bool
	// DeclarationMap writes .d.ts.map files when Declaration is on. Nil means no.
	DeclarationMap *bool
	// SourceMap writes .map files. Nil means no, matching tsc.
	SourceMap *bool
	// External package names or specifiers are left as import/require calls.
	External []string
	// Conditions are extra package.json "exports" conditions. The resolver
	// always adds import or require from the format, always matches default,
	// and always includes module. These values are appended on top.
	Conditions []string
	// Target is the TypeScript emit target. Empty means ESNext.
	Target core.ScriptTarget
	// Outputs lists Rollup-style emit targets. When set, each entry is built
	// separately (formats may resolve packages differently) and files merged.
	Outputs []OutputOptions
	// GlobalName is the IIFE/UMD global variable when Outputs is empty.
	GlobalName string
	// Define replaces free identifiers / process.env.* at emit time.
	Define map[string]string
	// Drop removes console.* calls and/or debugger statements ("console", "debugger").
	Drop []string
	// Alias maps import prefixes to filesystem paths (longest prefix wins).
	Alias map[string]string
	// Splitting emits async import() targets as separate chunks (app + OutDir).
	Splitting bool
	// CacheDir stores transpile results keyed by content hash.
	CacheDir string
	// Manifest writes a JSON map of source assets to emitted paths.
	Manifest bool
	// PublicPath prefixes emitted asset URLs for ?url and file imports.
	PublicPath string
	// Resolve, when it reports ok, supplies the file that specifier refers to.
	// Return ok false to keep the built-in resolver. The hook is how aliases
	// and, later, plugins replace resolution.
	Resolve func(importer, specifier string) (resolved string, ok bool, err error)
}

// Bundle reads Entry and returns the entry JavaScript.
// Declarations and source maps are included in [Build].
func Bundle(ctx context.Context, opts Options) (string, error) {
	result, err := Build(ctx, opts)
	if err != nil {
		return "", err
	}
	return result.JavaScript(), nil
}

// Output is one file produced by a build.
type Output struct {
	Path string
	Data string
}

// Result is every file a build wrote logically, including JavaScript,
// TypeScript declarations, and source maps.
type Result struct {
	Files    []Output
	Manifest map[string]string
	entry    string
}

// JavaScript returns the entry JavaScript source.
func (r *Result) JavaScript() string {
	if r == nil {
		return ""
	}
	if r.entry != "" {
		return r.entry
	}
	for _, file := range r.Files {
		if isJavaScriptOutput(file.Path) {
			return file.Data
		}
	}
	return ""
}

// Build reads Entry and returns the JavaScript, declarations, and source maps.
func Build(ctx context.Context, opts Options) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	outputs, err := opts.normalizedOutputs()
	if err != nil {
		return nil, err
	}
	if len(outputs) == 1 {
		return buildOnce(ctx, withOutput(opts, outputs[0]))
	}
	merged := &Result{Manifest: map[string]string{}}
	for _, out := range outputs {
		sub, err := buildOnce(ctx, withOutput(opts, out))
		if err != nil {
			return nil, err
		}
		merged.Files = append(merged.Files, sub.Files...)
		if merged.entry == "" {
			merged.entry = sub.entry
		}
		for k, v := range sub.Manifest {
			merged.Manifest[k] = v
		}
	}
	if opts.Manifest {
		merged.Files = append(merged.Files, manifestOutput(opts, merged.Manifest)...)
	}
	return merged, nil
}

func buildOnce(ctx context.Context, opts Options) (*Result, error) {
	roots, err := opts.absRoots()
	if err != nil {
		return nil, err
	}
	format, err := normalizeFormat(opts.Format)
	if err != nil {
		return nil, err
	}
	outMode, err := normalizeMode(opts.Mode)
	if err != nil {
		return nil, err
	}
	if err := validateOutputPaths(OutputOptions{Format: format, OutFile: opts.OutFile, OutDir: opts.OutDir}, outMode, len(roots)); err != nil {
		return nil, err
	}
	if outMode == ModeApp && !opts.Splitting && len(roots) != 1 {
		return nil, fmt.Errorf("single-file output requires one entry")
	}
	if opts.Splitting && outMode == ModeApp && opts.OutDir == "" {
		return nil, fmt.Errorf("--splitting requires --outdir")
	}
	jsx, err := jsxEmit(opts.JSX)
	if err != nil {
		return nil, err
	}
	entry := roots[0]
	resolution := core.ModuleKindESNext
	if format == FormatCJS {
		resolution = core.ModuleKindCommonJS
	}
	b := &bundler{
		ctx:       ctx,
		opts:      opts,
		format:    format,
		outMode:   outMode,
		jsx:       jsx,
		mode:      resolution,
		loading:   map[string]bool{},
		units:     map[string]*unit{},
		assets:    map[string]*asset.Unit{},
		decl:      opts.declaration() && outMode == ModeLib,
		declMap:   opts.declarationMap() && outMode == ModeLib,
		sourceMap: opts.sourceMap(),
		target:    opts.emitTarget(),
		cache:     newTranspileCache(opts.CacheDir),
	}
	res, err := resolve.New(filepath.Dir(entry), opts.Conditions, resolution, resolve.Config{
		Alias:       opts.Alias,
		External:    opts.External,
		Format:      string(format),
		LibBuild:    outMode == ModeLib,
		ResolveHook: opts.Resolve,
		OnAsset:     b.onAsset,
	})
	if err != nil {
		return nil, err
	}
	b.res = res
	for _, root := range roots {
		if err := b.load(root); err != nil {
			return nil, err
		}
	}
	for id, unit := range b.units {
		if unit.asset {
			continue
		}
		unit.code = applyDefineDrop(unit.code, opts.Define, opts.Drop)
		b.units[id] = unit
	}
	var result *Result
	if outMode == ModeLib {
		result, err = b.emitLibrary(entry)
	} else if opts.Splitting {
		result, err = b.emitSplit(roots)
	} else {
		linkFormat := format
		if format == FormatIIFE || format == FormatUMD {
			linkFormat = FormatCJS
			b.format = FormatCJS
		}
		var js string
		js, err = b.linkModules(moduleID(entry))
		if err != nil {
			return nil, err
		}
		b.format = format
		_ = linkFormat
		js = wrapFormat(js, format, opts.GlobalName)
		result, err = b.appResult(entry, js)
	}
	if err != nil {
		return nil, err
	}
	if result.Manifest == nil {
		result.Manifest = map[string]string{}
	}
	b.emitAssetFiles(result)
	if opts.Manifest && len(opts.Outputs) == 0 {
		result.Files = append(result.Files, manifestOutput(opts, result.Manifest)...)
	}
	return result, nil
}

type unit struct {
	code   string
	source string
	jsMap  string
	dts    string
	dtsMap string
	file   *ast.SourceFile
	json   bool
	asset  bool
}

type bundler struct {
	ctx       context.Context
	opts      Options
	format    Format
	outMode   Mode
	jsx       core.JsxEmit
	mode    core.ResolutionMode
	res     *resolve.Resolver
	loading map[string]bool
	units   map[string]*unit
	assets  map[string]*asset.Unit
	decl      bool
	declMap   bool
	sourceMap bool
	target    core.ScriptTarget
	cache     *cache.TranspileCache
	// chunkPaths maps module id -> relative chunk path for splitting.
	chunkPaths map[string]string
	// linkingChunk is the chunk path currently being linked (empty for entry).
	linkingChunk string
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
	if !asset.SupportedSource(path) {
		return fmt.Errorf("unsupported module %s", path)
	}
	if b.units[id] != nil && b.units[id].asset {
		delete(b.loading, id)
		return nil
	}

	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	srcBytes = bytes.TrimPrefix(srcBytes, []byte{0xEF, 0xBB, 0xBF})
	src := string(srcBytes)
	sourceText := src

	if strings.EqualFold(filepath.Ext(path), ".json") {
		if !json.Valid(srcBytes) {
			return fmt.Errorf("%s: invalid JSON", path)
		}
		payload := strings.TrimSpace(src)
		if b.outMode != ModeLib || b.format == FormatESM {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if !isIdentifierName(name) {
				name = "data"
			}
			text := "const " + name + " = " + payload + ";\nexport default " + name + ";\n"
			b.units[id] = &unit{code: text, source: sourceText, json: true, dts: "declare const " + name + ": any;\nexport default " + name + ";\n"}
			return nil
		}
		b.units[id] = &unit{code: "module.exports = " + payload + ";\n", source: sourceText, json: true}
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

	moduleKind := core.ModuleKindESNext
	if b.outMode == ModeLib && b.format == FormatCJS {
		moduleKind = core.ModuleKindCommonJS
	}
	sourceMapFlag := core.TSFalse
	if b.sourceMap {
		sourceMapFlag = core.TSTrue
	}
	cacheKey := ""
	var js, jsMap string
	cached := false
	if b.cache != nil {
		cacheKey = b.cache.Key(path, src, int(moduleKind), int(b.target), int(b.jsx), b.sourceMap)
		if ent, ok := b.cache.Get(cacheKey); ok {
			js, jsMap = ent.JS, ent.Map
			cached = true
		}
	}
	if !cached {
		out := transpile.TranspileModule(b.ctx, src, transpile.Options{
			FileName: filepath.Base(path),
			CompilerOptions: &core.CompilerOptions{
				Module:    moduleKind,
				Target:    b.target,
				Jsx:       b.jsx,
				SourceMap: sourceMapFlag,
			},
			ReportDiagnostics: true,
		})
		if out == nil {
			return fmt.Errorf("%s: transpile canceled", path)
		}
		if err := errorFromDiagnostics(path, src, out.Diagnostics); err != nil {
			return err
		}
		js = stripSourceMappingComment(stripShebang(out.OutputText))
		jsMap = out.SourceMapText
		if b.cache != nil && cacheKey != "" {
			b.cache.Put(cacheKey, cacheEntry{JS: js, Map: jsMap})
		}
	}
	if b.outMode == ModeLib {
		if moduleKind == core.ModuleKindESNext {
			emitted := parseEmittedJS(js)
			if err := errorFromDiagnostics(path, js, emitted.Diagnostics()); err != nil {
				return fmt.Errorf("parse emitted javascript: %w", err)
			}
			extra, err := b.depsFromSpecifiers(path, specifiers(emitted))
			if err != nil {
				return err
			}
			deps = append(deps, extra...)
		}
		b.units[id] = &unit{code: js, source: sourceText, jsMap: jsMap, file: file}
	} else if b.format == FormatESM || b.format == FormatCJS || b.format == FormatIIFE || b.format == FormatUMD {
		emitted := parseEmittedJS(js)
		if err := errorFromDiagnostics(path, js, emitted.Diagnostics()); err != nil {
			return fmt.Errorf("parse emitted javascript: %w", err)
		}
		extra, err := b.depsFromSpecifiers(path, specifiers(emitted))
		if err != nil {
			return err
		}
		deps = append(deps, extra...)
		b.units[id] = &unit{code: js, source: sourceText, jsMap: jsMap, file: file}
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

func (b *bundler) resolve(importer, spec string) (resolved string, external bool, err error) {
	return b.res.Resolve(importer, spec)
}

func (b *bundler) onAsset(abs string, q asset.Query) error {
	kind, ok := asset.KindForPath(abs, q)
	if !ok {
		return nil
	}
	return b.loadAsset(abs, kind)
}

func (b *bundler) loadAsset(abs string, kind asset.Kind) error {
	id := moduleID(abs)
	if _, ok := b.assets[id]; ok {
		return nil
	}
	au, js, err := asset.Load(abs, kind, b.opts.RootDir, b.opts.PublicPath)
	if err != nil {
		return err
	}
	b.assets[id] = au
	b.units[id] = &unit{code: js, source: string(au.Data), asset: true, json: false}
	return nil
}

func (b *bundler) emitAssetFiles(result *Result) {
	if result.Manifest == nil {
		result.Manifest = map[string]string{}
	}
	outRoot := b.opts.OutDir
	if outRoot == "" && b.opts.OutFile != "" {
		outRoot = filepath.Dir(b.opts.OutFile)
	}
	files, manifest := asset.CollectEmit(b.assets, outRoot)
	for _, f := range files {
		result.Files = append(result.Files, Output{Path: f.Path, Data: f.Data})
	}
	for k, v := range manifest {
		result.Manifest[k] = v
	}
}

func manifestOutput(opts Options, manifest map[string]string) []Output {
	if len(manifest) == 0 {
		manifest = map[string]string{}
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	path := asset.ManifestPath(opts.OutDir, opts.OutFile)
	return []Output{{Path: path, Data: string(raw) + "\n"}}
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

func (o Options) absRoots() ([]string, error) {
	var raw []string
	if o.Entry != "" {
		raw = append(raw, o.Entry)
	}
	raw = append(raw, o.Entries...)
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing entry file")
	}
	seen := map[string]bool{}
	var roots []string
	for _, path := range raw {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		id := moduleID(abs)
		if seen[id] {
			continue
		}
		seen[id] = true
		roots = append(roots, abs)
	}
	return roots, nil
}

func normalizeFormat(format Format) (Format, error) {
	switch format {
	case "", FormatESM:
		return FormatESM, nil
	case FormatCJS:
		return FormatCJS, nil
	case FormatIIFE:
		return FormatIIFE, nil
	case FormatUMD:
		return FormatUMD, nil
	default:
		return "", fmt.Errorf("unsupported format %q (use %s, %s, %s, or %s)", format, FormatESM, FormatCJS, FormatIIFE, FormatUMD)
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

func unitPaths(b *bundler, entry string) []string {
	paths := []string{entry}
	for id := range b.units {
		paths = append(paths, filepath.FromSlash(id))
	}
	return paths
}
