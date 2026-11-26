package bundler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/define"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/build/pkg/wrap"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/vfs"
)

func preferredLoadFormat(outputs []OutputOptions) Format {
	for _, out := range outputs {
		switch out.Format {
		case FormatESM, FormatIIFE, FormatUMD:
			return FormatESM
		}
	}
	return FormatCJS
}

// effectiveRootDir prefers Options.RootDir, then tsconfig rootDir, then the deepest directory shared by entries.
func effectiveRootDir(opts Options, cwd string) string {
	if opts.RootDir != "" {
		if abs, err := modpath.AbsPath(cwd, opts.RootDir); err == nil {
			return abs
		}
	}
	if opts.CompilerOptions != nil && opts.CompilerOptions.RootDir != "" {
		return filepath.ToSlash(opts.CompilerOptions.RootDir.AsString())
	}
	return ""
}

// newTranspileCache resolves dir against cwd so virtual builds keep cache entries in their own tree.
func newTranspileCache(fs vfs.FS, cwd, dir string) *cache.TranspileCache {
	if dir == "" {
		return nil
	}
	abs, err := modpath.AbsPath(cwd, dir)
	if err != nil {
		return nil
	}
	return cache.NewTranspileCache(fs, abs)
}

// transformKeyFor fingerprints transform plugins plus define/drop; other plugins cannot rewrite text.
func transformKeyFor(opts Options) string {
	names := make([]string, 0, len(opts.Plugins))
	for _, p := range opts.Plugins {
		if p.Transform != nil {
			names = append(names, p.Name)
		}
	}
	return transformCacheKey(names, opts.Define, opts.Drop)
}

func transformCacheKey(pluginNames []string, define map[string]string, drop []string) string {
	h := sha256.New()
	if err := transformCacheKeyWrite(h, pluginNames, define, drop); err != nil {
		// Fallback hashing keeps the key distinct even if the impossible encoding error occurs.
		h = sha256.New()
		_, _ = fmt.Fprintf(h, "%#v|%#v|%#v", pluginNames, define, drop)
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

func transformCacheKeyWrite(w io.Writer, pluginNames []string, define map[string]string, drop []string) error {
	names := append([]string(nil), pluginNames...)
	sort.Strings(names)
	drops := make([]string, 0, len(drop))
	for _, d := range drop {
		drops = append(drops, strings.ToLower(strings.TrimSpace(d)))
	}
	sort.Strings(drops)
	return json.MarshalWrite(w, struct {
		Plugins []string          `json:"plugins"`
		Define  map[string]string `json:"define"`
		Drop    []string          `json:"drop"`
	}{names, define, drops}, json.Deterministic(true))
}

func loadGraph(ctx context.Context, opts Options, outputs []OutputOptions) (*bundler, []string, error) {
	cwd := opts.CWD
	if cwd == "" {
		// A wasm build has no working directory; the caller supplies CWD there.
		cwd, _ = os.Getwd()
	}
	// Paths are compared as slash-separated vfs paths, so normalize cwd once here.
	cwd = filepath.ToSlash(cwd)
	roots, err := opts.absRoots(cwd)
	if err != nil {
		return nil, nil, err
	}
	rootDir := effectiveRootDir(opts, cwd)
	if err := define.Validate(opts.Define, opts.Drop); err != nil {
		return nil, nil, err
	}
	outMode, err := normalizeMode(opts.Mode)
	if err != nil {
		return nil, nil, err
	}
	jsx, err := jsxEmit(opts.JSX)
	if err != nil {
		return nil, nil, err
	}
	loadFormat := preferredLoadFormat(outputs)
	if err := validateBuildOutputs(opts, outputs, outMode, roots); err != nil {
		return nil, nil, err
	}
	entry := roots[0]
	resolution := core.ModuleKindESNext
	if loadFormat == FormatCJS {
		resolution = core.ModuleKindCommonJS
	}
	transpileCache := newTranspileCache(opts.FS, cwd, opts.CacheDir)
	b := &bundler{
		ctx:          ctx,
		opts:         opts,
		format:       loadFormat,
		outMode:      outMode,
		jsx:          jsx,
		mode:         resolution,
		fs:           opts.FS,
		cwd:          cwd,
		rootDir:      rootDir,
		loading:      map[string]bool{},
		units:        map[string]*unit{},
		assets:       map[string]*asset.Unit{},
		decl:         opts.declaration() && outMode == ModeLib,
		declMap:      opts.declarationMap() && outMode == ModeLib,
		sourceMap:    opts.sourceMap(),
		target:       opts.emitTarget(),
		compilerOpts: opts.CompilerOptions,
		cache:        transpileCache,
	}
	if b.cache != nil {
		// Transform settings are fixed per build, so fingerprint them once.
		b.transformKey = transformKeyFor(opts)
	}
	res, err := resolve.New(resolve.Config{
		Alias:           opts.Alias,
		External:        opts.External,
		Format:          string(loadFormat),
		LibBuild:        outMode == ModeLib && !opts.BundleNodeModules,
		EntryDir:        filepath.Dir(entry),
		Conditions:      opts.Conditions,
		Mode:            resolution,
		CompilerOptions: b.compilerOpts,
		BaseURL:         opts.BaseURL,
		ResolveHook:     combineResolveHook(ctx, opts.Plugins, opts.Resolve),
		OnAsset:         b.onAsset,
		FS:              b.fs,
		CWD:             b.cwd,
	})
	if err != nil {
		return nil, nil, err
	}
	b.res = res
	for _, root := range roots {
		if err := b.load(root); err != nil {
			return nil, nil, err
		}
	}
	// Plugin transforms ran during load, so only the define/drop rewrite remains.
	for id, unit := range b.units {
		if unit.asset {
			continue
		}
		rewritten, stages, err := define.ApplyDefineDrop(unit.code, opts.Define, opts.Drop)
		if err != nil {
			return nil, nil, err
		}
		unit.code = rewritten
		if len(stages) > 0 {
			if unit.verbatim && unit.jsMap.IsZero() {
				// A verbatim module maps line to line, so stages move it with the rewritten text.
				unit.jsMap = sourcemap.IdentitySourceMap("", "", unit.source, "", "")
			}
			// Each define/drop stage shifts the map until it describes the emitted module again.
			unit.jsMap = remapStages(unit.jsMap, stages)
			unit.file = nil
		}
		b.units[id] = unit
	}
	return b, roots, nil
}

// remapStages applies the define/drop edit stages to a module source map.
func remapStages(mapData *sourcemap.Map, stages []define.Stage) *sourcemap.Map {
	if mapData.IsZero() {
		return mapData
	}
	for _, stage := range stages {
		mapData = sourcemap.RemapEdits(mapData, stage.Source, stage.Edits)
	}
	return mapData
}

func validateBuildOutputs(opts Options, outputs []OutputOptions, outMode Mode, roots []string) error {
	if err := validateDepsDir(opts.DepsDir); err != nil {
		return err
	}
	for _, out := range outputs {
		if err := output.ValidateOutputPaths(out.OutFile, out.OutDir); err != nil {
			return err
		}
		if outMode == ModeApp {
			switch {
			case out.OutFile != "":
				if len(roots) != 1 {
					return fmt.Errorf("single-file output requires one entry")
				}
			case len(roots) > 1 && (out.Format == FormatIIFE || out.Format == FormatUMD):
				return fmt.Errorf("iife/umd output supports a single entry only")
			}
		}
		if (out.Format == FormatIIFE || out.Format == FormatUMD) && strings.TrimSpace(out.Name) == "" {
			return fmt.Errorf("iife/umd output requires --global-name")
		}
	}
	return nil
}

// validateDepsDir keeps bundled dependencies inside the output directory.
func validateDepsDir(depsDir string) error {
	if depsDir == "" {
		return nil
	}
	cleaned := path.Clean(filepath.ToSlash(depsDir))
	if cleaned == "." || cleaned == ".." || path.IsAbs(cleaned) ||
		strings.HasPrefix(cleaned, "../") || strings.ContainsAny(cleaned, ":\\") {
		return fmt.Errorf("deps dir must be a relative directory inside the output: %q", depsDir)
	}
	return nil
}

func (b *bundler) emitForFormat(opts Options, out OutputOptions, roots []string) (*Result, error) {
	prevOpts := b.opts
	prevFormat := b.format
	b.opts = withOutput(opts, out)
	b.opts.Outputs = nil
	b.format = out.Format
	defer func() {
		b.opts = prevOpts
		b.format = prevFormat
	}()

	entry := roots[0]

	var result *Result
	var err error
	if b.outMode == ModeLib {
		result, err = b.emitLibrary(entry)
	} else if b.opts.OutDir != "" {
		result, err = b.emitSplit(roots)
	} else {
		linkFmt := b.format
		if linkFmt == FormatIIFE || linkFmt == FormatUMD {
			b.format = FormatCJS
		}
		var js string
		js, err = b.linkModules(modpath.ModuleID(entry), nil)
		if err != nil {
			return nil, err
		}
		b.format = linkFmt
		linked := js
		global := out.Name
		if global == "" {
			global = opts.GlobalName
		}
		js = wrap.WrapFormat(js, string(linkFmt), global)
		result, err = b.appResult(entry, js, linked, linkFmt)
	}
	if err != nil {
		return nil, err
	}
	if result.Manifest == nil {
		result.Manifest = map[string]string{}
	}
	return result, nil
}
