// Package bundler builds a JavaScript or TypeScript program with tsgo and resolves imports through its bundler resolver.
// App mode links one bundle per entry; library mode emits one file per local module and leaves bare imports external.
// Externals stay as import/require calls, and source maps and declarations are off unless set, as in tsc.
// Options supports multiple outputs, Define/Drop, Alias, asset queries, CSS, a resolve hook, and a transpile cache.
// App mode with OutDir splits shared modules and import() targets into chunk files.
// Compiler options from tsconfig reach emit through Options.CompilerOptions.
// With Options.FS and Options.CWD, a build never uses the OS file system and can run on a virtual FS in wasm.
package bundler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/compileropts"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/plugin"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/binder"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	"github.com/startracex/tsgo/pkg/parser"
	"github.com/startracex/tsgo/pkg/transpile"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// Mode selects how output files are laid out.
type Mode string

const (
	// ModeApp writes one bundle per entry with inlined packages and no declarations.
	ModeApp Mode = "app"
	// ModeLib writes one file per local module and leaves bare package imports external.
	ModeLib Mode = "lib"
)

// Format selects the bundle's module format.
type Format string

const (
	// FormatESM emits import/export syntax; load it from .mjs or a "type":"module" package.
	FormatESM Format = "esm"
	// FormatCJS emits a script whose entry exports become module.exports under Node.
	FormatCJS Format = "cjs"
	// FormatIIFE wraps the bundle in an immediately-invoked function.
	FormatIIFE Format = "iife"
	// FormatUMD wraps the bundle in a Universal Module Definition.
	FormatUMD Format = "umd"
)

// JSX selects the JSX transform applied to .tsx and .jsx files.
type JSX string

const (
	// JSXReact is the default: it emits React.createElement without importing another package.
	JSXReact JSX = "react"
	// JSXAutomatic emits the react/jsx-runtime import, resolved and bundled like any dependency.
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
	// Alias maps import prefixes to filesystem paths (longest prefix wins).
	Alias map[string]string
	// CompilerOptions holds tsconfig options affecting emit and resolution; the bundler owns module kind, target, JSX, and source maps.
	CompilerOptions *core.CompilerOptions
	// Declaration writes .d.ts files in library mode only; nil disables it, matching tsc.
	Declaration *bool
	// DeclarationMap writes .d.ts.map files when Declaration is on; nil disables it.
	DeclarationMap *bool
	// Define replaces free identifiers / process.env.* at emit time.
	Define map[string]string
	// ManualChunks maps a module path substring to a chunk name under chunks/; app mode only.
	ManualChunks map[string]string
	// Resolve supplies the resolved file for a specifier; return ok=false to keep the built-in resolver.
	Resolve func(importer, specifier string) (resolved string, ok bool, err error)
	// SourceMap writes .map files. Nil means no, matching tsc.
	SourceMap *bool
	// BaseURL stands in for the removed tsconfig "baseUrl" option so old configs still resolve bare specifiers.
	BaseURL resolve.BaseURLFallback
	// Format selects ESM or CommonJS output. Empty means FormatESM.
	Format Format
	// FS reads sources, assets, and resolver probes; nil uses the OS file system, virtual builds supply their own.
	FS vfs.FS
	// JSX selects the JSX transform. Empty means JSXReact.
	JSX JSX
	// Mode selects app or lib layout; it must be set.
	Mode Mode
	// Target is the TypeScript emit target. Empty means ESNext.
	Target core.ScriptTarget
	// AssetFileNames names emitted assets and entry CSS; empty keeps the source path with a content hash.
	AssetFileNames string
	// CacheDir stores content-hashed transpile results relative to CWD through FS; empty disables the cache.
	CacheDir string
	// ChunkFileNames names split chunks in app mode; empty keeps the built-in chunks/ layout.
	ChunkFileNames string
	// CWD resolves relative entry and display paths; a wasm build may need to supply it.
	CWD string
	// DepsDir moves bundled packages under this directory in library mode; empty keeps node_modules.
	DepsDir string
	// Entry is the primary build root; Entries adds more, but single-file builds allow only one.
	Entry string
	// EntryFileNames is a per-entry filename pattern joined with OutDir.
	EntryFileNames string
	// GlobalName is the IIFE/UMD global variable when Outputs is empty.
	GlobalName string
	// NameTemplate marks OutFile as a full per-entry filename pattern.
	NameTemplate string
	// OutDir is the directory used by per-module output.
	OutDir string
	// OutFile is the app-mode JavaScript path; its map is written beside it.
	OutFile string
	// PublicPath prefixes emitted asset URLs for ?url and file imports.
	PublicPath string
	// RootDir bases paths under OutDir; it falls back to tsconfig rootDir, then the entries' deepest shared directory.
	RootDir string
	// Conditions appends package.json "exports" conditions; import/require, default, and module are always included.
	Conditions []string
	// Drop removes console.* calls and/or debugger statements ("console", "debugger").
	Drop []string
	// Entries adds root files; library mode emits every root and its imported modules.
	Entries []string
	// External package names or specifiers are left as import/require calls.
	External []string
	// Outputs lists Rollup-style emit targets sharing one loaded module graph.
	Outputs []OutputOptions
	// Plugins run Resolve/Load/Transform/GenerateBundle hooks; resolve/load take the first ok, others chain.
	Plugins []plugin.Plugin
	// BundleNodeModules keeps bare package imports inside library-mode output.
	BundleNodeModules bool
	// Manifest writes a JSON map of source assets to emitted paths.
	Manifest bool
}

// Bundle returns the entry JavaScript; use Build to get declarations and source maps too.
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

// Result holds every file a build wrote logically, including JavaScript, declarations, and maps.
type Result struct {
	Manifest map[string]string
	// Sources maps absolute source paths (slash separated) to emitted files.
	Sources map[string]string
	entry   string
	Files   []Output
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
		if modpath.IsJavaScriptOutput(file.Path) {
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
		// Only single-file output emits entry CSS here; split builds do it in emitSplit.
		if b.outMode == ModeApp && outputs[0].OutDir == "" {
			for _, root := range roots {
				b.emitEntryCSS(result, outputs[0].OutFile, root)
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
		nativePaths(result)
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
					jsOut = filepath.Join(out.OutDir, filepath.Base(modpath.JavascriptOutputAbs(root)))
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
	nativePaths(merged)
	return merged, nil
}

// nativePaths converts emitted slash paths to host separators for callers and os use.
func nativePaths(result *Result) {
	if result == nil {
		return
	}
	for i := range result.Files {
		result.Files[i].Path = filepath.Native(result.Files[i].Path)
	}
	for key, value := range result.Manifest {
		result.Manifest[key] = filepath.Native(value)
	}
	for key, value := range result.Sources {
		result.Sources[key] = filepath.Native(value)
	}
}

func genCacheKey(path, src, optionKey, transformKey string) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(src))
	h.Write([]byte{0})
	h.Write([]byte(optionKey))
	if transformKey != "" {
		h.Write([]byte{0})
		h.Write([]byte(transformKey))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type unit struct {
	file   *ast.SourceFile
	code   string
	dts    string
	dtsMap string
	jsMap  string
	source string
	asset  bool
	json   bool
	// verbatim marks a module whose emit only reformatted the source, keeping its text and identity map.
	verbatim bool
}

type bundler struct {
	assets map[string]*asset.Unit
	cache  *cache.TranspileCache
	// chunkPaths maps module id -> relative chunk path for splitting.
	chunkPaths map[string]string
	// compilerOpts holds tsconfig options that affect emit and resolution.
	compilerOpts *core.CompilerOptions
	// linkedMaps are per-module source maps remapped through the last Link.
	linkedMaps map[string]string
	loading    map[string]bool
	// outputPaths maps module id -> rendered output path so relative imports follow hashed names.
	outputPaths map[string]string
	res         *resolve.Resolver
	// sources maps source paths to rendered output paths.
	sources map[string]string
	units   map[string]*unit
	// usedOutputPaths deduplicates rendered names across outputs deterministically.
	usedOutputPaths map[string]bool
	ctx             context.Context
	format          Format
	// fs and cwd come from Options, defaulting to the OS file system and working directory.
	fs      vfs.FS
	jsx     core.JsxEmit
	mode    core.ResolutionMode
	outMode Mode
	target  core.ScriptTarget
	cwd     string
	// linkingChunk is the chunk path currently being linked (empty for entry).
	linkingChunk string
	rootDir      string
	// transformKey fingerprints plugin transform settings once per build.
	transformKey string
	opts         Options
	decl         bool
	declMap      bool
	sourceMap    bool
}

func (b *bundler) load(path string) error {
	id := modpath.ModuleID(path)
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
	abs, err := modpath.AbsPath(b.cwd, path)
	if err != nil {
		return err
	}
	path = abs
	id = modpath.ModuleID(path)
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
		srcBytes, err := modpath.ReadFile(b.fs, path)
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

	if modpath.LowerExt(path) == ".json" {
		if !jsontext.Value(src).IsValid() {
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
	if modpath.IsJavaScriptSource(path) {
		if err := errorFromSyntaxDiagnostics(path, src, file.JSDiagnostics()); err != nil {
			return err
		}
	} else if err := errorFromDiagnostics(path, src, file.JSDiagnostics()); err != nil {
		return err
	}
	binder.BindSourceFile(file)
	if !modpath.IsJavaScriptSource(path) {
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
	emitOpts := b.transpileOptionsFor(moduleKind, sourceMapFlag)
	cacheKey := ""
	var js, jsMap string
	cached := false
	if b.cache != nil {
		cacheKey = genCacheKey(path, src, emitOpts.Key(), b.transformKey)
		if ent, ok := b.cache.Get(cacheKey); ok {
			js, jsMap = ent.JS, ent.Map
			cached = true
		}
	}
	if !cached {
		out := transpile.TranspileModule(b.ctx, src, transpile.Options{
			FileName:          filepath.Base(path),
			CompilerOptions:   emitOpts.CompilerOptions(),
			ReportDiagnostics: true,
		})
		if out == nil {
			return fmt.Errorf("%s: transpile canceled", path)
		}
		if modpath.IsJavaScriptSource(path) {
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
	verbatim := false
	if b.outMode == ModeLib && moduleKind == core.ModuleKindCommonJS {
		// A CommonJS conversion makes the emitted text authoritative.
	} else {
		emitted := jsedit.ParseEmittedJS(js)
		if err := errorFromDiagnostics(path, js, emitted.Diagnostics()); err != nil {
			return fmt.Errorf("parse emitted javascript: %w", err)
		}
		if body, ok := b.verbatimModule(path, src, file, js, emitted); ok {
			// Keeping the source text preserves hand-minified code, comments, and formatting.
			js, jsMap, verbatim = body, "", true
		}
	}

	// Plugins run before imports are collected, so plugin-added imports are bundled like any other.
	// A plugin map composes onto the transpile map to keep the module pointing at its source.
	js, jsMap, transformed, err := b.runTransform(id, sourceText, js, jsMap)
	if err != nil {
		return err
	}
	if transformed {
		// The text no longer matches the parsed AST or the source text.
		verbatim = false
		file = nil
	}
	specs, err := b.specifiersFromCode(path, js, transformed)
	if err != nil {
		return err
	}
	extra, err := b.depsFromSpecifiers(path, specs)
	if err != nil {
		return err
	}
	deps = append(deps, extra...)
	b.units[id] = &unit{code: js, source: sourceText, jsMap: jsMap, file: file, verbatim: verbatim}

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

// verbatimModule returns source text only when emit is a pure reprint; downleveling, format changes, or removed comments make emit load-bearing.
func (b *bundler) verbatimModule(path, src string, file *ast.SourceFile, emittedText string, emitted *ast.SourceFile) (string, bool) {
	if !modpath.IsJavaScriptSource(path) {
		return "", false
	}
	if b.compilerOpts != nil && b.compilerOpts.RemoveComments.IsTrue() {
		return "", false
	}
	body := jsedit.StripSourceMappingComment(src)
	if body == "" || strings.HasPrefix(body, "#!") {
		return "", false
	}
	parsed := file
	if body != src {
		parsed = jsedit.ParseEmittedJS(body)
		if len(parsed.Diagnostics()) > 0 {
			return "", false
		}
	}
	if !jsedit.SameTokens(body, parsed, emittedText, emitted) {
		return "", false
	}
	return body, true
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
	au, js, err := asset.Load(b.fs, abs, kind, b.rootDir, b.opts.PublicPath, b.opts.AssetFileNames)
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
	var buf bytes.Buffer
	if err := json.MarshalWrite(&buf, manifest, json.Deterministic(true), jsontext.WithIndent("  ")); err != nil {
		buf.Reset()
	}
	path := asset.ManifestPath(opts.OutDir, opts.OutFile)
	return []Output{{Path: path, Data: buf.String() + "\n"}}
}

// runTransform applies plugin transforms and reports whether the text changed.
// A plugin map composes onto the transpile map; without one the transpile map is retained.
func (b *bundler) runTransform(id, sourceText, code, mapData string) (string, string, bool, error) {
	if len(b.opts.Plugins) == 0 {
		return code, mapData, false, nil
	}
	hookCtx := plugin.HookCtx{Context: b.ctx}
	out, outMap, err := plugin.RunTransform(b.opts.Plugins, hookCtx, code, id)
	if err != nil {
		return "", "", false, err
	}
	if out == code && outMap == "" {
		return code, mapData, false, nil
	}
	if strings.TrimSpace(outMap) != "" {
		if strings.TrimSpace(mapData) != "" {
			mapData = sourcemap.Compose(mapData, outMap, sourceText)
		} else {
			// A verbatim module has no transpile map, so only the file name needs retargeting.
			mapData = sourcemap.RetargetSourceMap(outMap, "", id, sourceText, "", "")
		}
	}
	return out, mapData, out != code, nil
}

// specifiersFromCode lists emitted imports; plugin output is checked so a syntax error cannot silently drop imports.
func (b *bundler) specifiersFromCode(path, code string, strict bool) ([]string, error) {
	parsed := jsedit.ParseEmittedJS(code)
	if strict {
		if err := errorFromDiagnostics(path, code, parsed.Diagnostics()); err != nil {
			return nil, fmt.Errorf("parse plugin output: %w", err)
		}
	}
	return specifiers(parsed), nil
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

func (o Options) absRoots(cwd string) ([]string, error) {
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
		abs, err := modpath.AbsPath(cwd, path)
		if err != nil {
			return nil, err
		}
		id := modpath.ModuleID(abs)
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

// errorFromSyntaxDiagnostics ignores semantic diagnostics because JavaScript is transformed, not type checked.
func errorFromSyntaxDiagnostics(path, src string, diags []*ast.Diagnostic) error {
	return errorFromDiagnosticsFiltered(path, src, diags, func(d *ast.Diagnostic) bool {
		return d.Code() < 2000
	})
}

// transpileOptionsFor collects emit-affecting options; compileropts shares one definition for options and cache fingerprint.
func (b *bundler) transpileOptionsFor(moduleKind core.ModuleKind, sourceMap core.Tristate) compileropts.Transpile {
	return compileropts.NewTranspile(moduleKind, b.target, b.jsx, sourceMap, b.compilerOpts)
}

func (b *bundler) transpileCompilerOptions(moduleKind core.ModuleKind, sourceMap core.Tristate) *core.CompilerOptions {
	return b.transpileOptionsFor(moduleKind, sourceMap).CompilerOptions()
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
			if modpath.IsJavaScriptOutput(f.Path) {
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
