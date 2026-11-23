package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/bundled"
	"github.com/startracex/tsgo/pkg/collections"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	"github.com/startracex/tsgo/pkg/tsoptions"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
	"github.com/urfave/cli/v2"
)

// applyTSConfig reads tsconfig.json and applies it to the build. It returns
// the compiler options from the config file so the bundler can use the ones
// that change the emitted code, the declarations, and module resolution.
func applyTSConfig(c *cli.Context, opts *bundler.Options, declaration, declarationMap *bool) (*core.CompilerOptions, error) {
	// Files on the command line (without -p) ignore tsconfig.json, matching
	// classic tsc / bundler CLI behavior.
	if c.String("project") == "" && (len(opts.Entries) > 0 || opts.Entry != "") {
		return nil, nil
	}
	path, err := tsconfigPath(c)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	parsed, configFileName, err := readTSConfig(path)
	if err != nil {
		return nil, err
	}
	compiler := parsed.CompilerOptions()
	opts.BaseURL = applyBaseURL(compiler, parsed, configFileName)
	if !c.IsSet("format") {
		opts.Format = formatFromModule(compiler.Module)
	}
	if !c.IsSet("jsx") {
		if jsx, ok := jsxFromCompiler(compiler.Jsx); ok {
			opts.JSX = jsx
		}
	}
	if !c.IsSet("map") {
		opts.SourceMap = new(compiler.SourceMap == core.TSTrue)
	}
	if !c.IsSet("dts") {
		*declaration = compiler.Declaration == core.TSTrue || compiler.Composite == core.TSTrue
	}
	*declarationMap = compiler.DeclarationMap == core.TSTrue
	if compiler.Target != core.ScriptTargetNone {
		opts.Target = compiler.Target
	}
	if !c.IsSet("conditions") && len(compiler.CustomConditions) > 0 {
		opts.Conditions = append([]string{}, compiler.CustomConditions...)
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
			return nil, err
		}
		opts.Entries = files
	}
	return compiler.Clone(), nil
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

func readTSConfig(path string) (*tsoptions.ParsedCommandLine, tspath.RootedFilePath, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	rooted, ok := tspath.TryRootedFilePathFromAbsolute(abs)
	if !ok {
		return nil, "", fmt.Errorf("invalid tsconfig path %s", abs)
	}
	fs := bundled.WrapFS(osvfs.FS())
	parsed, diags := tsoptions.GetParsedCommandLineOfConfigFile(rooted, nil, nil, tsoptions.NewDefaultParseConfigHost(fs), nil)
	if parsed == nil {
		if err := diagnosticError(diags); err != nil {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("cannot read %s", abs)
	}
	if err := diagnosticError(append(diags, parsed.Errors...)); err != nil {
		return nil, "", err
	}
	return parsed, rooted, nil
}

// applyBaseURL folds the removed "baseUrl" option into the "paths" mappings.
//
// TypeScript 5 resolved every "paths" substitution relative to "baseUrl" and
// used "baseUrl" as the root for bare specifiers. TypeScript 7 dropped
// "baseUrl": substitutions are resolved relative to the directory of the
// tsconfig that declared "paths", and bare specifiers have no project root to
// fall back to. The substitutions are rebased so that they keep naming the
// same files, and the wildcard mapping that stands in for the project root is
// returned to the caller.
//
// The value is read from the serialized config instead of the deprecated
// CompilerOptions field, which TypeScript 7 only allows inside option parsing
// and validation.
func applyBaseURL(compiler *core.CompilerOptions, parsed *tsoptions.ParsedCommandLine, configFileName tspath.RootedFilePath) resolve.BaseURLFallback {
	baseURL := tsconfigBaseURL(parsed, configFileName)
	if baseURL == "" {
		return resolve.BaseURLFallback{}
	}
	basePath := compiler.PathsBasePath
	if basePath == "" {
		basePath = configFileName.Directory()
	}
	compiler.PathsBasePath = basePath
	caseSensitivity := parsed.CaseSensitivity()
	paths := compiler.Paths
	if paths != nil {
		updated := collections.NewOrderedMapWithSizeHint[string, []string](paths.Size())
		for key, values := range paths.Entries() {
			rewritten := make([]string, len(values))
			for i, value := range values {
				rewritten[i] = rebasePathValue(value, baseURL, basePath, caseSensitivity)
			}
			updated.Set(key, rewritten)
		}
		compiler.Paths = updated
	}
	return resolve.BaseURLFallback{Directory: baseURL, CaseSensitivity: caseSensitivity}
}

// tsconfigBaseURL returns the directory of the "baseUrl" option, or an empty
// path when the config does not set it.
func tsconfigBaseURL(parsed *tsoptions.ParsedCommandLine, configFileName tspath.RootedFilePath) tspath.RootedDirectoryPath {
	serialized := tsoptions.ConvertToTSConfig(parsed, configFileName)
	if serialized == nil || serialized.CompilerOptions == nil {
		return ""
	}
	value, ok := serialized.CompilerOptions.Get("baseUrl")
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return ""
	}
	// The serialized value is relative to the config file.
	return tspath.ToRootedDirectoryPath(text, configFileName.Directory())
}

// rebasePathValue rewrites a "paths" substitution so that it stays relative to
// basePath while still naming the file the original substitution named under
// baseURL.
func rebasePathValue(value string, baseURL, basePath tspath.RootedDirectoryPath, caseSensitivity tspath.CaseSensitivity) string {
	if value == "" || tspath.IsRootedDiskPath(value) {
		return value
	}
	return resolve.RelativeTo(baseURL.ResolveFile(value), basePath, caseSensitivity)
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
	case core.JsxEmitReactJSX:
		return bundler.JSXAutomatic, true
	case core.JsxEmitReactJSXDev:
		return bundler.JSXReactJSXDev, true
	case core.JsxEmitPreserve:
		return bundler.JSXPreserve, true
	case core.JsxEmitReactNative:
		return bundler.JSXReactNative, true
	default:
		return "", false
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
