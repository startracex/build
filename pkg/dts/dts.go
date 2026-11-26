package dts

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/compileropts"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/tsgo/pkg/ast"
	"github.com/startracex/tsgo/pkg/bundled"
	"github.com/startracex/tsgo/pkg/compiler"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/diagnostics"
	"github.com/startracex/tsgo/pkg/locale"
	"github.com/startracex/tsgo/pkg/tsoptions"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
)

// Emit holds .d.ts text and optional declaration map for one source file.
type Emit struct {
	Text      string
	SourceMap string
}

// Options configures program-level declaration emit.
type Options struct {
	// CompilerOptions carries the tsconfig options affecting declaration output and type checking: strict family, "lib", "types", "paths", "moduleResolution", decorator and JSX settings; declaration-only switches are always set by this package.
	CompilerOptions *core.CompilerOptions
	// BaseURL stands in for the removed tsconfig "baseUrl" option so bare specifiers still resolve against the project root as in TypeScript 5.
	BaseURL resolve.BaseURLFallback
	// FS reads the program sources. Nil uses the OS file system.
	FS     vfs.FS
	Jsx    core.JsxEmit
	Target core.ScriptTarget
	// CWD resolves relative paths; empty falls back to the process working directory.
	CWD string
	// RootDir is the source root for declaration paths; empty falls back to the common directory of absPaths.
	RootDir          string
	CustomConditions []string
	DeclarationMap   bool
}

// declarationCompilerOptions honours tsconfig type-checking and inference options and always sets the declaration-only switches.
func declarationCompilerOptions(opts Options, declMap core.Tristate) *core.CompilerOptions {
	compilerOpts := &core.CompilerOptions{
		Declaration:          core.TSTrue,
		DeclarationMap:       declMap,
		EmitDeclarationOnly:  core.TSTrue,
		IsolatedDeclarations: core.TSFalse,
		NoCheck:              core.TSFalse,
		SkipLibCheck:         core.TSTrue,
		AllowJs:              core.TSTrue,
		ResolveJsonModule:    core.TSTrue,
		Module:               core.ModuleKindESNext,
		ModuleResolution:     core.ModuleResolutionKindBundler,
		Target:               opts.Target,
		Jsx:                  opts.Jsx,
		CustomConditions:     opts.CustomConditions,
		NoEmit:               core.TSFalse,
		NoEmitOnError:        core.TSFalse,
		Composite:            core.TSFalse,
		Incremental:          core.TSUnknown,
	}
	src := opts.CompilerOptions
	if src == nil {
		return compilerOpts
	}
	// Declaration emit reads shared syntax switches but not JavaScript-only ones: comments and line endings are not rewritten, and no helper is emitted.
	compileropts.SyntaxFrom(src).Apply(compilerOpts)
	compileropts.TypesFrom(src).Apply(compilerOpts)
	compileropts.ResolutionFrom(src, opts.BaseURL).Apply(compilerOpts)
	return compilerOpts
}

// EmitProgram runs a tsgo declaration-only program for the given paths; result keys are normalized via Key.
func EmitProgram(ctx context.Context, absPaths []string, opts Options) (map[string]Emit, error) {
	base := opts.FS
	if base == nil {
		base = osvfs.FS()
	}
	fs := bundled.WrapFS(base)
	sort.Strings(absPaths)
	var roots []tspath.RootedFilePath
	for _, abs := range absPaths {
		rooted, ok := tspath.TryRootedFilePathFromAbsolute(abs)
		if !ok {
			continue
		}
		roots = append(roots, rooted)
	}
	if len(roots) == 0 {
		return map[string]Emit{}, nil
	}

	declMap := core.TSFalse
	if opts.DeclarationMap {
		declMap = core.TSTrue
	}
	compilerOpts := declarationCompilerOptions(opts, declMap)
	sourceRoot := opts.RootDir
	if sourceRoot == "" {
		sourceRoot = modpath.DisplayRoot(opts.CWD, absPaths)
	}
	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)
	program, err := compiler.NewProgram(ctx, compiler.ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(compilerOpts, roots, nil, tspath.RootedDirectoryPathFromAbsolute(sourceRoot), fs.CaseSensitivity()),
		Host:           host,
		SingleThreaded: core.TSTrue,
	})
	if err != nil {
		return nil, err
	}

	out := map[string]Emit{}
	result := program.Emit(ctx, compiler.EmitOptions{
		EmitOnly:  compiler.EmitOnlyDts,
		ForceEmit: true,
		WriteFile: func(fileName tspath.RootedFilePath, text string, data *compiler.WriteFileData) error {
			if data == nil || data.SourceFile == nil {
				return nil
			}
			key := Key(data.SourceFile.FileName().AsString())
			slot := out[key]
			if strings.HasSuffix(strings.ToLower(fileName.AsString()), ".map") {
				slot.SourceMap = text
			} else {
				slot.Text = text
			}
			out[key] = slot
			return nil
		},
	})
	if result == nil {
		return nil, fmt.Errorf("declaration emit canceled")
	}
	if len(out) == 0 {
		if err := ErrorFromDiagnostics("declaration", "", result.Diagnostics); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("declaration emit produced no files")
	}
	return out, nil
}

// Key normalizes a filesystem path for declaration emit maps.
func Key(path string) string {
	// The compiler reports absolute paths; only a relative one needs the working directory, which a wasm build may lack.
	abs := filepath.FromSlash(path)
	if !filepath.IsAbs(abs) {
		if cleaned, err := filepath.Abs(abs); err == nil {
			abs = cleaned
		}
	}
	id := modpath.ModuleID(abs)
	if runtime.GOOS == "windows" {
		id = strings.ToLower(id)
	}
	return id
}

// ErrorFromDiagnostics formats TypeScript diagnostics as an error.
func ErrorFromDiagnostics(path, src string, diags []*ast.Diagnostic) error {
	var b strings.Builder
	n := 0
	for _, d := range diags {
		if d == nil || d.Category() != diagnostics.CategoryError {
			continue
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		line, col := lineCol(src, d.Pos())
		fmt.Fprintf(&b, "%s:%d:%d: TS%d: %s", path, line, col, d.Code(), d.Localize(locale.Default))
		n++
	}
	if n == 0 {
		return nil
	}
	return fmt.Errorf("%s", b.String())
}

func lineCol(src string, pos int) (int, int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(src) {
		pos = len(src)
	}
	line, col := 1, 1
	for i := 0; i < pos; i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}
