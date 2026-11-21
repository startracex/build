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
		Description: "With no outfile, build reads tsconfig.json and emits one file per module.\n" +
			"   --outfile with one entry writes a single bundle (or several with repeated --format).\n" +
			"   --outdir accepts one entry, many entries, or a glob.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "outfile", Aliases: []string{"o"}, Usage: "write one bundle for a single entry"},
			&cli.StringFlag{Name: "outdir", Usage: "directory for per-module or split output"},
			&cli.StringFlag{Name: "project", Aliases: []string{"p"}, Usage: "tsconfig.json file or directory"},
			&cli.StringSliceFlag{Name: "format", Usage: "output format: esm, cjs, iife, umd (repeatable)"},
			&cli.StringFlag{Name: "jsx", Usage: "JSX transform: react or react-jsx"},
			&cli.StringFlag{Name: "global-name", Usage: "global variable name for iife/umd"},
			&cli.StringSliceFlag{Name: "conditions", Usage: "extra package.json exports conditions (repeatable)"},
			&cli.StringSliceFlag{Name: "alias", Usage: "import alias from=to (repeatable)"},
			&cli.StringSliceFlag{Name: "define", Usage: "replace identifier KEY=VAL (repeatable)"},
			&cli.StringSliceFlag{Name: "drop", Usage: "drop console or debugger (repeatable)"},
			&cli.BoolFlag{Name: "sourcemap", Usage: "write JavaScript source maps"},
			&cli.BoolFlag{Name: "dts", Usage: "write .d.ts files for per-module output"},
			&cli.BoolFlag{Name: "declarationMap", Usage: "write .d.ts.map files when declarations are enabled"},
			&cli.StringSliceFlag{Name: "external", Usage: "package left unbundled (repeatable)"},
			&cli.BoolFlag{Name: "splitting", Usage: "emit import() targets as chunks (requires --outdir)"},
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

func action(c *cli.Context) error {
	if c.String("outfile") != "" && c.String("outdir") != "" {
		return errors.New("--outfile and --outdir cannot both be set")
	}
	entries, err := expandEntries(c.Args().Slice())
	if err != nil {
		return err
	}
	if err := rejectConfigWithFiles(c, entries); err != nil {
		return err
	}

	formats := c.StringSlice("format")
	formatSet := c.IsSet("format")
	if len(formats) == 0 {
		formats = []string{string(bundler.FormatESM)}
	}
	alias := parseKV(c.StringSlice("alias"))
	define := parseKV(c.StringSlice("define"))
	base := bundler.Options{
		JSX:        bundler.JSX(flagOr(c, "jsx", string(bundler.JSXReact))),
		External:   c.StringSlice("external"),
		Conditions: c.StringSlice("conditions"),
		Alias:      alias,
		Define:     define,
		Drop:       c.StringSlice("drop"),
		GlobalName: c.String("global-name"),
		Splitting:  c.Bool("splitting"),
		CacheDir:   c.String("cache-dir"),
		Manifest:   c.Bool("manifest"),
		PublicPath: c.String("public-path"),
	}
	if c.IsSet("sourcemap") {
		base.SourceMap = new(c.Bool("sourcemap"))
	}

	if outfile := c.String("outfile"); outfile != "" {
		if len(entries) != 1 {
			return fmt.Errorf("--outfile requires a single entry, got %d", len(entries))
		}
		base.Entry = entries[0]
		base.Mode = bundler.ModeApp
		if len(formats) == 1 {
			if formatSet {
				base.Format = bundler.Format(formats[0])
			} else {
				base.Format = bundler.FormatESM
			}
			base.OutFile = outfile
			return writeBuild(c, base)
		}
		for _, f := range formats {
			format := bundler.Format(f)
			base.Outputs = append(base.Outputs, bundler.OutputOptions{
				Format:  format,
				OutFile: bundler.DeriveOutFile(outfile, format),
				Name:    base.GlobalName,
			})
		}
		return writeBuild(c, base)
	}

	base.Entries = entries
	base.Mode = bundler.ModeLib
	if c.Bool("splitting") {
		base.Mode = bundler.ModeApp
	}
	decl := false
	declMap := false
	if err := applyTSConfig(c, &base, &decl, &declMap); err != nil {
		return err
	}
	if c.IsSet("jsx") {
		base.JSX = bundler.JSX(c.String("jsx"))
	}
	if c.IsSet("sourcemap") {
		base.SourceMap = new(c.Bool("sourcemap"))
	}
	if c.IsSet("dts") {
		decl = c.Bool("dts")
	}
	if c.IsSet("declarationMap") {
		declMap = c.Bool("declarationMap")
	}
	base.Declaration = new(decl)
	base.DeclarationMap = new(decl && declMap)

	outdir := c.String("outdir")
	if outdir == "" {
		outdir = base.OutDir
	}
	if len(formats) == 1 {
		if formatSet {
			base.Format = bundler.Format(formats[0])
		}
		base.OutDir = outdir
	} else {
		base.Outputs = nil
		for _, f := range formats {
			format := bundler.Format(f)
			base.Outputs = append(base.Outputs, bundler.OutputOptions{
				Format: format,
				OutDir: bundler.DeriveOutDir(outdir, format),
				Name:   base.GlobalName,
			})
		}
		base.OutDir = outdir
	}
	if len(base.Entries) == 0 && base.Entry == "" {
		return errors.New("tsconfig.json not found")
	}
	return writeBuild(c, base)
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

func writeBuild(c *cli.Context, opts bundler.Options) error {
	result, err := bundler.Build(context.Background(), opts)
	if err != nil {
		return err
	}
	for _, file := range result.Files {
		if dir := filepath.Dir(file.Path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(file.Path, []byte(file.Data), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(c.App.Writer, "wrote %s\n", file.Path)
	}
	return nil
}

func containsGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[{")
}
