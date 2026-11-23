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
//
// The compiler options read from tsconfig.json reach the build through the
// context, see Options.CompilerOptions. They decide the emitted JavaScript,
// the declaration output, and how module specifiers are resolved.
package bundler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/plugin"
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
	// JSXReactJSXDev emits the development react/jsx-dev-runtime import.
	JSXReactJSXDev JSX = "react-jsxdev"
	// JSXPreserve keeps JSX syntax in the emitted output.
	JSXPreserve JSX = "preserve"
	// JSXReactNative keeps JSX syntax like preserve, with React Native typing.
	JSXReactNative JSX = "react-native"
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
	// NameTemplate marks OutFile as a full per-entry filename pattern.
	NameTemplate string
	// EntryFileNames is a per-entry filename pattern joined with OutDir.
	EntryFileNames string
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
	// BundleNodeModules keeps bare package imports inside library-mode output.
	BundleNodeModules bool
	// Conditions are extra package.json "exports" conditions. The resolver
	// always adds import or require from the format, always matches default,
	// and always includes module. These values are appended on top.
	Conditions []string
	// Target is the TypeScript emit target. Empty means ESNext.
	Target core.ScriptTarget
	// CompilerOptions holds the compiler options read from tsconfig.json. They
	// decide the emitted JavaScript, the declaration output, and how module
	// specifiers are resolved. The bundler owns module kind, target, JSX
	// transform, and source maps; the rest comes from here.
	CompilerOptions *core.CompilerOptions
	// BaseURL stands in for the removed tsconfig "baseUrl" option. TypeScript
	// 5 resolved bare specifiers against it, so a tsconfig that still uses it
	// keeps working.
	BaseURL resolve.BaseURLFallback
	// Outputs lists Rollup-style emit targets. The module graph is loaded once;
	// each output is linked/emitted from that shared graph.
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
	// ManualChunks maps a module path substring to a chunk base name under
	// chunks/ (e.g. "node_modules/lodash" -> "vendor-lodash"). App split only.
	ManualChunks map[string]string
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
	// Plugins run Resolve/Load/Transform/GenerateBundle hooks (first ok wins
	// for resolve/load; transform and generate chain).
	Plugins []plugin.Plugin
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
	// Sources maps absolute source paths (slash separated) to emitted files.
	Sources map[string]string
	entry   string
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
	b, roots, err := loadGraph(ctx, opts, outputs)
	if err != nil {
		return nil, err
	}
	b.usedOutputPaths = map[string]bool{}
	b.sources = map[string]string{}
	if len(outputs) == 1 {
		result, err := b.emitForFormat(opts, outputs[0], roots)
		if err != nil {
			return nil, err
		}
		b.emitAssetFiles(result)
		if b.outMode == ModeApp {
			for _, root := range roots {
				jsOut := outputs[0].OutFile
				if jsOut == "" && outputs[0].OutDir != "" {
					jsOut = filepath.Join(outputs[0].OutDir, filepath.Base(javascriptOutputAbs(root)))
				}
				b.emitEntryCSS(result, jsOut, root)
			}
		}
		if opts.Manifest && len(opts.Outputs) == 0 {
			result.Files = append(result.Files, manifestOutput(opts, result.Manifest)...)
		}
		hookCtx := plugin.HookCtx{Context: ctx}
		if err := applyGenerateBundle(opts.Plugins, hookCtx, result); err != nil {
			return nil, err
		}
		result.Sources = b.sources
		return result, nil
	}
	merged := &Result{Manifest: map[string]string{}, Sources: b.sources}
	for _, out := range outputs {
		sub, err := b.emitForFormat(opts, out, roots)
		if err != nil {
			return nil, err
		}
		if b.outMode == ModeApp {
			for _, root := range roots {
				jsOut := out.OutFile
				if jsOut == "" && out.OutDir != "" {
					jsOut = filepath.Join(out.OutDir, filepath.Base(javascriptOutputAbs(root)))
				}
				b.emitEntryCSS(sub, jsOut, root)
			}
		}
		merged.Files = append(merged.Files, sub.Files...)
		if merged.entry == "" {
			merged.entry = sub.entry
		}
		maps.Copy(merged.Manifest, sub.Manifest)
	}
	save := b.opts
	b.opts = withOutput(opts, outputs[0])
	b.emitAssetFiles(merged)
	b.opts = save
	if opts.Manifest {
		merged.Files = append(merged.Files, manifestOutput(opts, merged.Manifest)...)
	}
	hookCtx := plugin.HookCtx{Context: ctx}
	if err := applyGenerateBundle(opts.Plugins, hookCtx, merged); err != nil {
		return nil, err
	}
	return merged, nil
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
	mode      core.ResolutionMode
	res       *resolve.Resolver
	loading   map[string]bool
	units     map[string]*unit
	assets    map[string]*asset.Unit
	decl      bool
	declMap   bool
	sourceMap bool
	target    core.ScriptTarget
	// compilerOpts holds the compiler options read from tsconfig.json. They
	// change the emitted JavaScript, the declaration output, and how module
	// specifiers are resolved.
	compilerOpts *core.CompilerOptions
	cache        *cache.TranspileCache
	// outputPaths maps module id -> rendered output path when using a
	// filename template. It keeps relative imports aligned with hashed names.
	outputPaths map[string]string
	// usedOutputPaths tracks every rendered path across outputs so duplicate
	// names are renamed deterministically instead of overwriting files.
	usedOutputPaths map[string]bool
	// sources maps source paths to rendered output paths.
	sources map[string]string
	// chunkPaths maps module id -> relative chunk path for splitting.
	chunkPaths map[string]string
	// linkingChunk is the chunk path currently being linked (empty for entry).
	linkingChunk string
	// linkedMaps are per-module source maps remapped through the last Link.
	linkedMaps map[string]string
}

func (b *bundler) load(path string) error {
	id := moduleID(path)
	if b.loading[id] {
		return nil
	}
	if _, done := b.units[id]; done {
		return nil
	}
	// Virtual asset ids (path?raw / path?url) are registered in onAsset.
	if strings.Contains(path, "?") {
		return fmt.Errorf("missing asset module %s", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	path = abs
	id = moduleID(path)
	if b.loading[id] {
		return nil
	}
	if _, done := b.units[id]; done {
		return nil
	}
	b.loading[id] = true
	if b.units[id] != nil && b.units[id].asset {
		delete(b.loading, id)
		return nil
	}

	hookCtx := plugin.HookCtx{Context: b.ctx}
	var src string
	var sourceText string
	loadedFromPlugin := false
	if code, ok, err := plugin.RunLoad(b.opts.Plugins, hookCtx, id); err != nil {
		return err
	} else if ok {
		src = code
		sourceText = code
		loadedFromPlugin = true
	} else {
		if !asset.SupportedSource(path) {
			return fmt.Errorf("unsupported module %s", path)
		}
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		srcBytes = bytes.TrimPrefix(srcBytes, []byte{0xEF, 0xBB, 0xBF})
		src = string(srcBytes)
		sourceText = src
	}
	parsePath := path
	if loadedFromPlugin && modpath.Ext(path) == "" {
		parsePath = path + ".js"
	}

	if modpath.IsJSONPath(parsePath) {
		if !json.Valid([]byte(src)) {
			return fmt.Errorf("%s: invalid JSON", path)
		}
		payload := strings.TrimSpace(src)
		if b.outMode != ModeLib || b.format == FormatESM {
			name := modpath.BaseStem(path)
			if !jsedit.IsIdentifierName(name) {
				name = "data"
			}
			text := "const " + name + " = " + payload + ";\nexport default " + name + ";\n"
			b.units[id] = &unit{code: text, source: sourceText, json: true, dts: "declare const " + name + ": any;\nexport default " + name + ";\n"}
			return nil
		}
		b.units[id] = &unit{code: "module.exports = " + payload + ";\n", source: sourceText, json: true}
		return nil
	}

	file, err := parseSource(parsePath, src)
	if err != nil {
		return err
	}
	if err := errorFromDiagnostics(path, src, file.Diagnostics()); err != nil {
		return err
	}
	if isJavaScriptSource(path) {
		if err := errorFromSyntaxDiagnostics(path, src, file.JSDiagnostics()); err != nil {
			return err
		}
	} else if err := errorFromDiagnostics(path, src, file.JSDiagnostics()); err != nil {
		return err
	}
	binder.BindSourceFile(file)
	if !isJavaScriptSource(path) {
		if err := errorFromDiagnostics(path, src, file.BindDiagnostics()); err != nil {
			return err
		}
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
		cacheKey = b.cache.Key(path, src, int(moduleKind), int(b.target), int(b.jsx), b.sourceMap, b.transformKey(), b.compilerOptionsKey())
		if ent, ok := b.cache.Get(cacheKey); ok {
			js, jsMap = ent.JS, ent.Map
			cached = true
		}
	}
	if !cached {
		out := transpile.TranspileModule(b.ctx, src, transpile.Options{
			FileName:          filepath.Base(path),
			CompilerOptions:   b.transpileCompilerOptions(moduleKind, sourceMapFlag),
			ReportDiagnostics: true,
		})
		if out == nil {
			return fmt.Errorf("%s: transpile canceled", path)
		}
		if isJavaScriptSource(path) {
			if err := errorFromSyntaxDiagnostics(path, src, out.Diagnostics); err != nil {
				return err
			}
		} else if err := errorFromDiagnostics(path, src, out.Diagnostics); err != nil {
			return err
		}
		js = jsedit.StripSourceMappingComment(jsedit.StripShebang(out.OutputText))
		jsMap = out.SourceMapText
		if b.cache != nil && cacheKey != "" {
			b.cache.Put(cacheKey, cache.Entry{JS: js, Map: jsMap})
		}
	}
	if b.outMode == ModeLib {
		if moduleKind == core.ModuleKindESNext {
			emitted := jsedit.ParseEmittedJS(js)
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
		emitted := jsedit.ParseEmittedJS(js)
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
	id := asset.ModuleKey(abs, kind)
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
	for _, au := range b.assets {
		if au.Kind == asset.CSS && b.outMode == ModeApp {
			continue
		}
		if au.Kind == asset.Raw {
			result.Manifest[au.Source] = "(inline)"
			continue
		}
		dest := filepath.Join(outRoot, filepath.FromSlash(au.OutRel))
		result.Files = append(result.Files, Output{Path: dest, Data: string(au.Data)})
		result.Manifest[au.Source] = dest
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
	case JSXReactJSXDev:
		return core.JsxEmitReactJSXDev, nil
	case JSXPreserve:
		return core.JsxEmitPreserve, nil
	case JSXReactNative:
		return core.JsxEmitReactNative, nil
	default:
		return core.JsxEmitNone, fmt.Errorf("unsupported jsx mode %q (use %s, %s, %s, %s, or %s)", mode, JSXReact, JSXAutomatic, JSXReactJSXDev, JSXPreserve, JSXReactNative)
	}
}

func errorFromDiagnostics(path, src string, diags []*ast.Diagnostic) error {
	return errorFromDiagnosticsFiltered(path, src, diags, nil)
}

// errorFromSyntaxDiagnostics ignores semantic (@typedef, lib conflicts, etc.)
// diagnostics. JavaScript sources are transformed, not type checked.
func errorFromSyntaxDiagnostics(path, src string, diags []*ast.Diagnostic) error {
	return errorFromDiagnosticsFiltered(path, src, diags, func(d *ast.Diagnostic) bool {
		return d.Code() < 2000
	})
}

// transpileCompilerOptions returns the compiler options used to emit the
// JavaScript of one module. It starts from the tsconfig options that change
// the emitted code and overrides the ones the bundler controls (module kind,
// target, JSX transform, and source maps).
func (b *bundler) transpileCompilerOptions(moduleKind core.ModuleKind, sourceMap core.Tristate) *core.CompilerOptions {
	opts := &core.CompilerOptions{
		Module:    moduleKind,
		Target:    b.target,
		Jsx:       b.jsx,
		SourceMap: sourceMap,
	}
	src := b.compilerOpts
	if src == nil {
		return opts
	}
	opts.ExperimentalDecorators = src.ExperimentalDecorators
	opts.EmitDecoratorMetadata = src.EmitDecoratorMetadata
	opts.UseDefineForClassFields = src.UseDefineForClassFields
	opts.VerbatimModuleSyntax = src.VerbatimModuleSyntax
	opts.IsolatedModules = src.IsolatedModules
	opts.ImportHelpers = src.ImportHelpers
	opts.NoEmitHelpers = src.NoEmitHelpers
	opts.JsxFactory = src.JsxFactory
	opts.JsxFragmentFactory = src.JsxFragmentFactory
	opts.JsxImportSource = src.JsxImportSource
	opts.ReactNamespace = src.ReactNamespace
	opts.PreserveConstEnums = src.PreserveConstEnums
	opts.RemoveComments = src.RemoveComments
	if src.NewLine != core.NewLineKindNone {
		opts.NewLine = src.NewLine
	}
	return opts
}

// compilerOptionsKey hashes the tsconfig options that change the emitted
// JavaScript. It keeps the transpile cache from reusing output that was
// emitted with different settings.
func (b *bundler) compilerOptionsKey() string {
	src := b.compilerOpts
	if src == nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "ed=%d;md=%d;ud=%d;vm=%d;im=%d;ih=%d;neh=%d;jf=%s;jff=%s;fis=%s;rn=%s;pce=%d;rc=%d;nl=%d\n",
		int(src.ExperimentalDecorators),
		int(src.EmitDecoratorMetadata),
		int(src.UseDefineForClassFields),
		int(src.VerbatimModuleSyntax),
		int(src.IsolatedModules),
		int(src.ImportHelpers),
		int(src.NoEmitHelpers),
		src.JsxFactory,
		src.JsxFragmentFactory,
		src.JsxImportSource,
		src.ReactNamespace,
		int(src.PreserveConstEnums),
		int(src.RemoveComments),
		int(src.NewLine),
	)
	return hex.EncodeToString(h.Sum(nil))
}

func isJavaScriptSource(path string) bool {
	return modpath.IsJavaScriptSource(path)
}

func errorFromDiagnosticsFiltered(path, src string, diags []*ast.Diagnostic, keep func(*ast.Diagnostic) bool) error {
	var b strings.Builder
	n := 0
	for _, d := range diags {
		if d == nil || d.Category() != diagnostics.CategoryError {
			continue
		}
		if keep != nil && !keep(d) {
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

func combineResolveHook(ctx context.Context, plugins []plugin.Plugin, user func(importer, specifier string) (resolved string, ok bool, err error)) func(importer, spec string) (string, bool, bool, error) {
	hookCtx := plugin.HookCtx{Context: ctx}
	return func(importer, spec string) (string, bool, bool, error) {
		id, external, ok, err := plugin.RunResolve(plugins, hookCtx, spec, importer)
		if err != nil || ok {
			return id, external, ok, err
		}
		if user != nil {
			path, uok, uerr := user(importer, spec)
			return path, false, uok, uerr
		}
		return "", false, false, nil
	}
}

func applyGenerateBundle(plugins []plugin.Plugin, ctx plugin.HookCtx, result *Result) error {
	if len(plugins) == 0 {
		return nil
	}
	files := make([]plugin.File, len(result.Files))
	for i, f := range result.Files {
		files[i] = plugin.File{Path: f.Path, Data: f.Data}
	}
	out, err := plugin.RunGenerateBundle(plugins, ctx, files)
	if err != nil {
		return err
	}
	result.Files = result.Files[:0]
	for _, f := range out {
		result.Files = append(result.Files, Output{Path: f.Path, Data: f.Data})
	}
	if result.entry != "" {
		for _, f := range result.Files {
			if isJavaScriptOutput(f.Path) {
				result.entry = f.Data
				break
			}
		}
	}
	return nil
}

func unitPaths(b *bundler, entry string) []string {
	paths := []string{entry}
	for id := range b.units {
		paths = append(paths, filepath.FromSlash(id))
	}
	return paths
}
