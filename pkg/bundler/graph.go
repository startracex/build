package bundler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/cache"
	"github.com/startracex/build/pkg/define"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/plugin"
	"github.com/startracex/build/pkg/resolve"
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

func (b *bundler) transformKey() string {
	names := make([]string, 0, len(b.opts.Plugins))
	for _, p := range b.opts.Plugins {
		if p.Transform != nil {
			names = append(names, p.Name)
		}
	}
	return cache.TransformCacheKey(names, b.opts.Define, b.opts.Drop)
}

func loadGraph(ctx context.Context, opts Options, outputs []OutputOptions) (*bundler, []string, error) {
	roots, err := opts.absRoots()
	if err != nil {
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
	res, err := resolve.New(filepath.Dir(entry), opts.Conditions, resolution, resolve.Config{
		Alias:           opts.Alias,
		External:        opts.External,
		Format:          string(loadFormat),
		LibBuild:        outMode == ModeLib && !opts.BundleNodeModules,
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
		unit.code = define.ApplyDefineDrop(code, opts.Define, opts.Drop)
		b.units[id] = unit
	}
	return b, roots, nil
}

func validateBuildOutputs(opts Options, outputs []OutputOptions, outMode Mode, roots []string) error {
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
