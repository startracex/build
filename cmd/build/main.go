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
			"   --outfile with one entry writes a single bundle. Several entries with --outfile are an error.\n" +
			"   --outdir accepts one entry, many entries, or a glob.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "outfile", Aliases: []string{"o"}, Usage: "write one bundle for a single entry"},
			&cli.StringFlag{Name: "outdir", Usage: "directory for per-module output"},
			&cli.StringFlag{Name: "project", Aliases: []string{"p"}, Usage: "tsconfig.json file or directory"},
			&cli.StringFlag{Name: "format", Usage: "output format: esm or cjs"},
			&cli.StringFlag{Name: "jsx", Usage: "JSX transform: react or react-jsx"},
			&cli.StringSliceFlag{Name: "conditions", Usage: "extra package.json exports conditions (repeatable)"},
			&cli.BoolFlag{Name: "sourcemap", Usage: "write JavaScript source maps"},
			&cli.BoolFlag{Name: "dts", Usage: "write .d.ts files for per-module output"},
			&cli.BoolFlag{Name: "declarationMap", Usage: "write .d.ts.map files when declarations are enabled"},
			&cli.StringSliceFlag{Name: "external", Usage: "package left unbundled (repeatable)"},
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
	external := c.StringSlice("external")
	if err := rejectConfigWithFiles(c, entries); err != nil {
		return err
	}

	if outfile := c.String("outfile"); outfile != "" {
		if len(entries) != 1 {
			return fmt.Errorf("--outfile requires a single entry, got %d", len(entries))
		}
		opts := bundler.Options{
			Entry:      entries[0],
			Format:     bundler.Format(flagOr(c, "format", string(bundler.FormatESM))),
			JSX:        bundler.JSX(flagOr(c, "jsx", string(bundler.JSXReact))),
			Mode:       bundler.ModeApp,
			OutFile:    outfile,
			External:   external,
			Conditions: c.StringSlice("conditions"),
		}
		if c.IsSet("sourcemap") {
			opts.SourceMap = new(c.Bool("sourcemap"))
		}
		return writeBuild(c, opts)
	}

	opts := bundler.Options{
		Entries:    entries,
		Mode:       bundler.ModeLib,
		OutDir:     c.String("outdir"),
		Format:     bundler.FormatESM,
		JSX:        bundler.JSXReact,
		External:   external,
		Conditions: c.StringSlice("conditions"),
	}
	decl := false
	declMap := false
	if err := applyTSConfig(c, &opts, &decl, &declMap); err != nil {
		return err
	}
	if c.IsSet("format") {
		opts.Format = bundler.Format(c.String("format"))
	}
	if c.IsSet("jsx") {
		opts.JSX = bundler.JSX(c.String("jsx"))
	}
	if c.IsSet("sourcemap") {
		opts.SourceMap = new(c.Bool("sourcemap"))
	}
	if c.IsSet("dts") {
		decl = c.Bool("dts")
	}
	if c.IsSet("declarationMap") {
		declMap = c.Bool("declarationMap")
	}
	opts.Declaration = new(decl)
	opts.DeclarationMap = new(decl && declMap)
	if len(opts.Entries) == 0 && opts.Entry == "" {
		return errors.New("tsconfig.json not found")
	}
	return writeBuild(c, opts)
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
