// Command build bundles a JavaScript or TypeScript entry file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/bundler"
)

func main() {
	if err := run(os.Args[1:], os.Stderr, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "build: %s\n", err)
		os.Exit(1)
	}
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func run(args []string, stderr, stdout io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	if stdout == nil {
		stdout = io.Discard
	}
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outfile := fs.String("outfile", "", "app mode: file to write the bundle to")
	shortOut := fs.String("o", "", "app mode: file to write the bundle to")
	entryFlag := fs.String("entry", "", "entry module")
	jsx := fs.String("jsx", string(bundler.JSXReact), "JSX transform: react or react-jsx")
	format := fs.String("format", string(bundler.FormatESM), "output format: esm or cjs")
	mode := fs.String("mode", string(bundler.ModeApp), "output layout: app (one file) or lib (one file per module)")
	outdir := fs.String("outdir", "", "lib mode: directory for JavaScript, declarations, and source maps")
	platform := fs.String("platform", "node", "package export condition: node or browser")
	sourcemap := fs.Bool("sourcemap", true, "write source maps (default true)")
	dts := fs.Bool("dts", true, "library mode: write .d.ts and .d.ts.map with tsgo (default true)")
	var external stringList
	fs.Var(&external, "external", "package or specifier to leave unbundled (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage:\n  build -o outfile entry\n  build --mode lib --outdir dist entry\n\n")
		fmt.Fprintf(stderr, "App mode writes one bundle. Library mode writes one file per local module and leaves package imports external.\n")
		fmt.Fprintf(stderr, "Source maps are written next to the JavaScript. Library mode also writes .d.ts and .d.ts.map. App mode does not write declarations.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	entry := *entryFlag
	rest := fs.Args()
	switch {
	case entry != "" && len(rest) > 0:
		return fmt.Errorf("entry given twice (%s and %s)", entry, rest[0])
	case entry == "" && len(rest) == 1:
		entry = rest[0]
	case entry == "" && len(rest) == 0:
		return errors.New("missing entry file")
	case len(rest) > 1:
		return errors.New("too many arguments")
	}

	output := *outfile
	if *shortOut != "" {
		if output != "" && output != *shortOut {
			return errors.New("-o and --outfile disagree")
		}
		output = *shortOut
	}
	switch bundler.Mode(*mode) {
	case "", bundler.ModeApp:
		if output == "" {
			return errors.New("missing output file (-o or --outfile)")
		}
	case bundler.ModeLib:
		if *outdir == "" {
			return errors.New("missing output directory (--outdir)")
		}
	default:
		return fmt.Errorf("unsupported mode %q (use app or lib)", *mode)
	}

	result, err := bundler.Build(context.Background(), bundler.Options{
		Entry:       entry,
		Format:      bundler.Format(*format),
		JSX:         bundler.JSX(*jsx),
		Mode:        bundler.Mode(*mode),
		OutFile:     output,
		OutDir:      *outdir,
		Declaration: dts,
		SourceMap:   sourcemap,
		External:    []string(external),
		Platform:    *platform,
	})
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
		fmt.Fprintf(stdout, "wrote %s\n", file.Path)
	}
	return nil
}
