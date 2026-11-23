// Command build bundles a JavaScript or TypeScript program.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/resolve"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args[1:], os.Stderr, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "build: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string, stderr, stdout io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	if stdout == nil {
		stdout = io.Discard
	}
	app := &cli.App{
		Name:  "build",
		Usage: "Bundle JavaScript and TypeScript",
		Description: "With no output path, build reads tsconfig.json and emits one file per module.\n" +
			"   --outfile writes bundles for a single entry; --outdir writes per-module or split output.\n" +
			"   --mode app|lib selects the layout; it is inferred from the output path when omitted.\n" +
			"   Output paths and --format are repeatable and aligned from the right.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "outfile", Aliases: []string{"o"}, Usage: "single-file bundle path (repeatable)"},
			&cli.StringSliceFlag{Name: "outdir", Aliases: []string{"d"}, Usage: "directory for per-module or split output (repeatable)"},
			&cli.StringFlag{Name: "entry-file-names", Aliases: []string{"n"}, Usage: "filename pattern for --outdir outputs ([name], [path], [format], [hash], [hash:N])"},
			&cli.StringFlag{Name: "root-dir", Aliases: []string{"r"}, Usage: "base directory for paths written under the output directory"},
			&cli.StringFlag{Name: "project", Aliases: []string{"p"}, Usage: "tsconfig.json file or directory"},
			&cli.StringSliceFlag{Name: "format", Aliases: []string{"f"}, Usage: "output format: esm, cjs, iife, umd (repeatable)"},
			&cli.StringFlag{Name: "jsx", Usage: "JSX transform: react, react-jsx, react-jsxdev, preserve, or react-native"},
			&cli.StringFlag{Name: "global-name", Aliases: []string{"g"}, Usage: "global variable name for iife/umd"},
			&cli.StringSliceFlag{Name: "conditions", Usage: "extra package.json exports conditions (repeatable)"},
			&cli.StringFlag{Name: "platform", Usage: "runtime for the JavaScript output: browser, node, or neutral (default browser for app mode)"},
			&cli.StringSliceFlag{Name: "alias", Usage: "import alias from=to (repeatable)"},
			&cli.StringSliceFlag{Name: "define", Usage: "replace a free reference KEY=EXPR (repeatable)"},
			&cli.StringSliceFlag{Name: "drop", Usage: "drop console calls or debugger statements (repeatable)"},
			&cli.BoolFlag{Name: "map", Usage: "write source maps; with --dts also writes .d.ts.map"},
			&cli.BoolFlag{Name: "dts", Usage: "write .d.ts files for per-module output"},
			&cli.StringSliceFlag{Name: "external", Usage: "package left unbundled (repeatable)"},
			&cli.StringFlag{Name: "mode", Usage: "output layout: app (one bundle per entry) or lib (one file per module)"},
			&cli.BoolFlag{Name: "bundle", Aliases: []string{"bundle-node-modules"}, Usage: "bundle bare package imports in library output"},
			&cli.StringFlag{Name: "deps-dir", Usage: "directory for bundled packages in library output (default node_modules)"},
			&cli.StringFlag{Name: "cache-dir", Usage: "directory for transpile cache"},
			&cli.BoolFlag{Name: "manifest", Usage: "write manifest.json of emitted assets"},
			&cli.StringFlag{Name: "public-path", Usage: "URL prefix for emitted assets"},
		},
		Writer:         stdout,
		ErrWriter:      stderr,
		ExitErrHandler: func(*cli.Context, error) {},
		Action:         action,
	}
	return app.Run(append([]string{"build"}, args...))
}

// resolveMode reads the explicit --mode value. An empty value keeps the
// layout derived from the output paths.
func resolveMode(value string) (bundler.Mode, error) {
	switch value {
	case "":
		return "", nil
	case string(bundler.ModeApp):
		return bundler.ModeApp, nil
	case string(bundler.ModeLib):
		return bundler.ModeLib, nil
	default:
		return "", fmt.Errorf("unsupported mode %q (use %s or %s)", value, bundler.ModeApp, bundler.ModeLib)
	}
}

// warnIgnoredFlags reports flags that the selected layout makes pointless.
func warnIgnoredFlags(c *cli.Context, opts *bundler.Options, declaration bool) {
	if opts.Mode != bundler.ModeApp {
		return
	}
	if opts.BundleNodeModules {
		fmt.Fprintln(c.App.ErrWriter, "build: warning: --bundle has no effect in app mode, package imports are bundled already")
	}
	if opts.DepsDir != "" {
		fmt.Fprintln(c.App.ErrWriter, "build: warning: --deps-dir has no effect in app mode, packages are inlined into the bundle")
	}
	switch {
	case c.IsSet("dts"):
		fmt.Fprintln(c.App.ErrWriter, "build: warning: --dts has no effect in app mode, app output has no declarations")
	case declaration:
		fmt.Fprintln(c.App.ErrWriter, "build: warning: tsconfig declaration is ignored in app mode, app output has no declarations")
	}
}

func action(c *cli.Context) error {
	outfiles := c.StringSlice("outfile")
	outdirs := c.StringSlice("outdir")
	if len(outfiles) > 0 && len(outdirs) > 0 {
		return errors.New("--outfile and --outdir cannot both be set")
	}
	explicitMode, err := resolveMode(c.String("mode"))
	if err != nil {
		return err
	}
	entries, err := expandEntries(c.Args().Slice())
	if err != nil {
		return err
	}
	htmlDocs, entries, err := prepareHTMLEntries(entries)
	if err != nil {
		return err
	}

	explicitFormats := c.StringSlice("format")
	formatSet := c.IsSet("format")
	alias := parseKV(c.StringSlice("alias"))
	define := parseKV(c.StringSlice("define"))
	// The platform decides how the module graph is resolved, and a build loads
	// that graph once, so every output shares one platform.
	platform, err := resolve.ParsePlatform(c.String("platform"))
	if err != nil {
		return err
	}
	base := bundler.Options{
		JSX:               bundler.JSX(flagOr(c, "jsx", string(bundler.JSXReact))),
		External:          c.StringSlice("external"),
		BundleNodeModules: c.Bool("bundle"),
		DepsDir:           c.String("deps-dir"),
		Conditions:        c.StringSlice("conditions"),
		Platform:          platform,
		Alias:             alias,
		Define:            define,
		Drop:              c.StringSlice("drop"),
		GlobalName:        c.String("global-name"),
		CacheDir:          c.String("cache-dir"),
		Manifest:          c.Bool("manifest"),
		PublicPath:        c.String("public-path"),
		EntryFileNames:    c.String("entry-file-names"),
		RootDir:           c.String("root-dir"),
	}

	if len(entries) == 1 {
		base.Entry = entries[0]
	} else {
		base.Entries = entries
	}
	if len(htmlDocs) > 0 && base.RootDir == "" {
		base.RootDir = filepath.Dir(htmlDocs[0].path)
	}
	base.Format = bundler.FormatESM
	declaration, declarationMap := false, false
	compilerOpts, err := applyTSConfig(c, &base, &declaration, &declarationMap)
	if err != nil {
		return err
	}
	base.CompilerOptions = compilerOpts
	ctx := context.Background()
	if formatSet && len(explicitFormats) == 1 {
		base.Format = bundler.Format(explicitFormats[0])
	}
	if c.IsSet("jsx") {
		base.JSX = bundler.JSX(c.String("jsx"))
	}
	applyEmitFlags(c, &base, &declaration, &declarationMap)

	if len(outfiles) > 0 {
		base.EntryFileNames = ""
		templated := false
		for _, outfile := range outfiles {
			if templateOf(outfile) != "" {
				templated = true
				break
			}
		}
		entryCount := len(base.Entries)
		if base.Entry != "" {
			entryCount++
		}
		if entryCount == 0 {
			return errors.New("--outfile requires at least one entry")
		}
		if !templated && entryCount != 1 {
			return fmt.Errorf("--outfile requires a single entry, got %d", entryCount)
		}
		if len(outfiles) > 1 {
			cleaned, err := uniqueFiles(outfiles)
			if err != nil {
				return err
			}
			if len(cleaned) != len(outfiles) {
				return errors.New("duplicate --outfile paths")
			}
		}
		switch {
		case explicitMode == bundler.ModeApp:
			if entryCount != 1 {
				return errors.New("--mode app with --outfile needs a single entry; use --outdir for several entries")
			}
			base.Mode = bundler.ModeApp
		case explicitMode == bundler.ModeLib:
			return errors.New("--mode lib needs --outdir; --outfile writes a single bundle")
		case entryCount == 1:
			base.Mode = bundler.ModeApp
		default:
			base.Mode = bundler.ModeLib
		}
		plan, err := planOutputs(outputFile, outfiles, formatList(explicitFormats, formatSet, base.Format), base.Format, "", base.GlobalName)
		if err != nil {
			return err
		}
		applyOutputPlan(&base, plan)
		warnIgnoredFlags(c, &base, declaration)
		return writeBuild(c, ctx, base, htmlDocs)
	}

	switch explicitMode {
	case bundler.ModeApp:
		base.Mode = bundler.ModeApp
	case bundler.ModeLib:
		base.Mode = bundler.ModeLib
	default:
		base.Mode = bundler.ModeLib
	}
	if base.EntryFileNames != "" && len(outdirs) == 0 && base.OutDir == "" {
		return errors.New("--entry-file-names requires --outdir")
	}
	plan, err := planOutputs(outputDir, outdirs, formatList(explicitFormats, formatSet, base.Format), base.Format, base.OutDir, base.GlobalName)
	if err != nil {
		return err
	}
	applyOutputPlan(&base, plan)
	if len(base.Entries) == 0 && base.Entry == "" {
		return errors.New("tsconfig.json not found")
	}
	warnIgnoredFlags(c, &base, declaration)
	return writeBuild(c, ctx, base, htmlDocs)
}

func applyEmitFlags(c *cli.Context, opts *bundler.Options, declaration, declarationMap *bool) {
	mapSet := c.IsSet("map")
	mapEnabled := c.Bool("map")
	dtsSet := c.IsSet("dts")
	dtsEnabled := c.Bool("dts")
	if mapSet {
		opts.SourceMap = new(mapEnabled)
	}
	if dtsSet {
		*declaration = dtsEnabled
	}
	if mapSet && dtsSet {
		*declarationMap = mapEnabled && dtsEnabled
	}
	opts.Declaration = new(*declaration)
	opts.DeclarationMap = new(*declaration && *declarationMap)
}

func formatList(explicit []string, set bool, fallback bundler.Format) []bundler.Format {
	if set && len(explicit) > 0 {
		list := make([]bundler.Format, len(explicit))
		for i, value := range explicit {
			list[i] = bundler.Format(value)
		}
		return list
	}
	return []bundler.Format{fallback}
}

func parseKV(pairs []string) map[string]string {
	out := map[string]string{}
	for _, p := range pairs {
		key, val, ok := strings.Cut(p, "=")
		if !ok || key == "" {
			continue
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func flagOr(c *cli.Context, name, fallback string) string {
	if c.IsSet(name) {
		return c.String(name)
	}
	if value := c.String(name); value != "" {
		return value
	}
	return fallback
}

func writeBuild(c *cli.Context, ctx context.Context, opts bundler.Options, docs []*htmlDocument) error {
	result, err := bundler.Build(ctx, opts)
	if err != nil {
		return err
	}
	if len(docs) > 0 {
		extra, err := renderHTMLDocuments(docs, opts, result)
		if err != nil {
			return err
		}
		result.Files = append(result.Files, extra...)
	}
	for _, file := range result.Files {
		if dir := filepath.Dir(file.Path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		if err := writeFileString(file.Path, file.Data); err != nil {
			return err
		}
		fmt.Fprintf(c.App.Writer, "wrote %s\n", file.Path)
	}
	return nil
}

// writeFileString writes an output without converting it to a byte slice, which
// would copy the whole file.
func writeFileString(path, data string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func containsGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[{")
}
