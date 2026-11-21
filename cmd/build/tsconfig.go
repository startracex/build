package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/bundled"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	"github.com/startracex/tsgo/pkg/tsoptions"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
	"github.com/urfave/cli/v2"
)

func rejectConfigWithFiles(c *cli.Context, entries []string) error {
	if c.String("project") != "" {
		if len(entries) > 0 {
			return errors.New("option 'project' cannot be mixed with source files on a command line")
		}
		return nil
	}
	if c.Bool("ignoreConfig") && len(entries) > 0 {
		return nil
	}
	found, err := findTSConfig()
	if err != nil {
		return err
	}
	if found != "" && len(entries) > 0 {
		return errors.New("tsconfig.json is present but will not be loaded if files are specified on the command line; use --ignoreConfig to skip this error")
	}
	return nil
}

func applyTSConfig(c *cli.Context, opts *bundler.Options, declaration *bool) error {
	if c.Bool("ignoreConfig") && (len(opts.Entries) > 0 || opts.Entry != "") {
		return nil
	}
	path, err := tsconfigPath(c)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	parsed, err := readTSConfig(path)
	if err != nil {
		return err
	}
	compiler := parsed.CompilerOptions()
	if !c.IsSet("format") {
		opts.Format = formatFromModule(compiler.Module)
	}
	if !c.IsSet("jsx") {
		if jsx, ok := jsxFromCompiler(compiler.Jsx); ok {
			opts.JSX = jsx
		}
	}
	if !c.IsSet("sourcemap") {
		opts.SourceMap = new(tristateOr(compiler.SourceMap, true))
	}
	if !c.IsSet("dts") {
		*declaration = tristateOr(compiler.Declaration, true)
	}
	if opts.OutDir == "" && compiler.OutDir != "" {
		opts.OutDir = filepath.FromSlash(compiler.OutDir.AsString())
	}
	if compiler.RootDir != "" {
		opts.RootDir = filepath.FromSlash(compiler.RootDir.AsString())
	} else if common := parsed.CommonSourceDirectory(); common != "" {
		opts.RootDir = filepath.FromSlash(common.AsString())
	}
	if len(opts.Entries) == 0 && opts.Entry == "" {
		files, err := configFiles(parsed)
		if err != nil {
			return err
		}
		opts.Entries = files
	}
	return nil
}

func tsconfigPath(c *cli.Context) (string, error) {
	if path := c.String("project"); path != "" {
		return resolveProjectPath(path)
	}
	return findTSConfig()
}

// findTSConfig walks from the working directory through each ancestor, the same
// order as TypeScript's findConfigFile.
func findTSConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "tsconfig.json")
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func resolveProjectPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("the specified path does not exist: %s", abs)
		}
		return "", err
	}
	if info.IsDir() {
		config := filepath.Join(abs, "tsconfig.json")
		info, err = os.Stat(config)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("cannot find a tsconfig.json file at the specified directory: %s", abs)
		}
		return config, nil
	}
	return abs, nil
}

func readTSConfig(path string) (*tsoptions.ParsedCommandLine, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(abs)
	if !ok {
		return nil, fmt.Errorf("invalid tsconfig path %s", abs)
	}
	fs := bundled.WrapFS(osvfs.FS())
	parsed, diags := tsoptions.GetParsedCommandLineOfConfigFile(rooted, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
	if parsed == nil {
		if err := diagnosticError(diags); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("cannot read %s", abs)
	}
	if err := diagnosticError(append(diags, parsed.Errors...)); err != nil {
		return nil, err
	}
	return parsed, nil
}

func configFiles(parsed *tsoptions.ParsedCommandLine) ([]string, error) {
	var files []string
	for _, name := range parsed.FileNames() {
		path := filepath.FromSlash(name.AsString())
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".d.ts") {
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".json":
			files = append(files, path)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("tsconfig.json did not match any input files")
	}
	return files, nil
}

func formatFromModule(kind core.ModuleKind) bundler.Format {
	if kind == core.ModuleKindCommonJS {
		return bundler.FormatCJS
	}
	return bundler.FormatESM
}

func jsxFromCompiler(kind core.JsxEmit) (bundler.JSX, bool) {
	switch kind {
	case core.JsxEmitReact:
		return bundler.JSXReact, true
	case core.JsxEmitReactJSX, core.JsxEmitReactJSXDev:
		return bundler.JSXAutomatic, true
	default:
		return "", false
	}
}

func tristateOr(value core.Tristate, fallback bool) bool {
	switch value {
	case core.TSTrue:
		return true
	case core.TSFalse:
		return false
	default:
		return fallback
	}
}

func diagnosticError(diags []*ast.Diagnostic) error {
	var b strings.Builder
	n := 0
	for _, diag := range diags {
		if diag == nil || diag.Category() != diagnostics.CategoryError {
			continue
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "TS%d: %s", diag.Code(), diag.Localize(locale.Default))
		n++
	}
	if n == 0 {
		return nil
	}
	return errors.New(b.String())
}
