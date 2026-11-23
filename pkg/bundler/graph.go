package bundler

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/define"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/plugin"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/build/pkg/wrap"
	"github.com/startracex/tsgo/pkg/core"
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

// resolutionPlatform fills in the platform when the caller left it open. A
// single-file ESM bundle runs in a browser; every other layout keeps the
// tsconfig semantics so Node consumers and declaration emit stay consistent.
func resolutionPlatform(platform resolve.Platform, mode Mode, format Format) resolve.Platform {
	if platform != resolve.PlatformAuto {
		return platform
	}
	if mode == ModeApp && format == FormatESM {
		return resolve.PlatformBrowser
	}
	return resolve.PlatformNeutral
}

// transformKeyFor fingerprints the plugin names and define/drop values that
// change the transform step. Only a plugin with a transform hook can rewrite
// source text, so the others are left out.
func transformKeyFor(opts Options) string {
	names := make([]string, 0, len(opts.Plugins))
	for _, p := range opts.Plugins {
		if p.Transform != nil {
			names = append(names, p.Name)
		}
	}
	return cache.TransformCacheKey(names, opts.Define, opts.Drop)
}

func loadGraph(ctx context.Context, opts Options, outputs []OutputOptions) (*bundler, []string, error) {
	roots, err := opts.absRoots()
	if err != nil {
		return nil, nil, err
	}
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
	b := &bundler{
		ctx:          ctx,
		opts:         opts,
		format:       loadFormat,
		outMode:      outMode,
		jsx:          jsx,
		mode:         resolution,
		loading:      map[string]bool{},
		units:        map[string]*unit{},
		assets:       map[string]*asset.Unit{},
		decl:         opts.declaration() && outMode == ModeLib,
		declMap:      opts.declarationMap() && outMode == ModeLib,
		sourceMap:    opts.sourceMap(),
		target:       opts.emitTarget(),
		compilerOpts: opts.CompilerOptions,
		cache:        cache.NewTranspileCache(opts.CacheDir),
	}
	if b.cache != nil {
		// The transform settings are fixed for a build, so fingerprint them
		// once instead of once per module.
		b.transformKey = transformKeyFor(opts)
	}
	res, err := resolve.New(filepath.Dir(entry), opts.Conditions, resolution, resolve.Config{
		Alias:           opts.Alias,
		External:        opts.External,
		Format:          string(loadFormat),
		LibBuild:        outMode == ModeLib && !opts.BundleNodeModules,
		Platform:        resolutionPlatform(opts.Platform, outMode, loadFormat),
		CompilerOptions: b.compilerOpts,
		BaseURL:         opts.BaseURL,
		ResolveHook:     combineResolveHook(ctx, opts.Plugins, opts.Resolve),
		OnAsset:         b.onAsset,
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
	hookCtx := plugin.HookCtx{Context: ctx}
	for id, unit := range b.units {
		if unit.asset {
			continue
		}
		code, _, err := plugin.RunTransform(opts.Plugins, hookCtx, unit.code, id)
		if err != nil {
			return nil, nil, err
		}
		rewritten, stages, err := define.ApplyDefineDrop(code, opts.Define, opts.Drop)
		if err != nil {
			return nil, nil, err
		}
		unit.code = rewritten
		if len(stages) > 0 {
			if unit.verbatim && strings.TrimSpace(unit.jsMap) == "" {
				// A verbatim module maps line to line; the stages then move it
				// along with the text they rewrite.
				unit.jsMap = sourcemap.IdentitySourceMap("", "", unit.source)
			}
			// The map describes the code before the rewrite, so each stage
			// shifts it until it describes the emitted module again.
			unit.jsMap = remapStages(unit.jsMap, stages)
			unit.file = nil
		}
		b.units[id] = unit
	}
	return b, roots, nil
}

// remapStages applies the define/drop edit stages to a module source map.
func remapStages(mapData string, stages []define.Stage) string {
	if strings.TrimSpace(mapData) == "" {
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

// validateDepsDir keeps the bundled dependency directory inside the output
// directory, so it cannot escape it or depend on the current drive.
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
