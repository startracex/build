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

	"github.com/startracex/build/bundler"
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

func run(args []string, stderr, stdout io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}
	if stdout == nil {
		stdout = io.Discard
	}
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outfile := fs.String("outfile", "", "file to write the bundle to")
	shortOut := fs.String("o", "", "file to write the bundle to")
	entryFlag := fs.String("entry", "", "entry module")
	jsx := fs.String("jsx", string(bundler.JSXReact), "JSX transform: react or react-jsx")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage:\n  build -o outfile entry\n  build --outfile outfile --entry entry\n\n")
		fmt.Fprintf(stderr, "Bundle a JavaScript or TypeScript entry and its relative imports into one script.\n\n")
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
	if output == "" {
		return errors.New("missing output file (-o or --outfile)")
	}

	code, err := bundler.Bundle(context.Background(), bundler.Options{
		Entry: entry,
		JSX:   bundler.JSX(*jsx),
	})
	if err != nil {
		return err
	}
	if dir := filepath.Dir(output); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(output, []byte(code), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", output)
	return nil
}
