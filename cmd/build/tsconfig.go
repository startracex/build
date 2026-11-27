package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/modpath"
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

// applyTSConfig applies tsconfig.json and returns compiler options affecting emitted code, declarations and module resolution.
func applyTSConfig(c *cli.Context, opts *bundler.Options, out *bundler.OutputOptions, declaration, declarationMap *bool) (*core.CompilerOptions, error) {
	// Files on the command line (without -p) ignore tsconfig.json, matching classic tsc CLI behavior.
	if c.String("project") == "" && len(opts.Entries) > 0 {
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
		out.Format = formatFromModule(compiler.Module)
	}
	if !c.IsSet("map") {
		out.SourceMap = new(compiler.SourceMap == core.TSTrue)
	}
	if !c.IsSet("dts") {
		*declaration = compiler.Declaration == core.TSTrue || compiler.Composite == core.TSTrue
	}
	*declarationMap = compiler.DeclarationMap == core.TSTrue
	if !c.IsSet("conditions") && len(compiler.CustomConditions) > 0 {
		opts.Conditions = append([]string{}, compiler.CustomConditions...)
	}
	if out.OutDir == "" && compiler.OutDir != "" {
		out.OutDir = filepath.FromSlash(compiler.OutDir.AsString())
	}
	if !c.IsSet("root-dir") {
		if compiler.RootDir != "" {
			opts.RootDir = filepath.FromSlash(compiler.RootDir.AsString())
		} else if common := parsed.CommonSourceDirectory(); common != "" {
			opts.RootDir = filepath.FromSlash(common.AsString())
		}
	}
	if len(opts.Entries) == 0 {
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

// findTSConfig walks up from the working directory, matching TypeScript's findConfigFile order.
func findTSConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findTSConfigIn(dir)
}

// findTSConfigIn walks up from dir so callers need not change the process working directory.
func findTSConfigIn(dir string) (string, error) {
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

// applyBaseURL folds the removed "baseUrl" into "paths" so substitutions keep naming the same files.
// The wildcard for the project root is returned for bare specifiers, which no longer have a fallback root.
// The value comes from the serialized config because the deprecated field is only valid during option parsing.
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

// tsconfigBaseURL returns the "baseUrl" directory, or empty when unset.
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

// rebasePathValue keeps a "paths" substitution relative to basePath while naming the same file under baseURL.
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
		if modpath.IsDeclaration(path) {
			continue
		}
		if asset.IsSource(path) {
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
