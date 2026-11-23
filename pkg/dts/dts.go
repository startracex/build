package dts

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
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
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
)

// Emit holds .d.ts text and optional declaration map for one source file.
type Emit struct {
	Text      string
	SourceMap string
}

// Options configures program-level declaration emit.
type Options struct {
	Target           core.ScriptTarget
	Jsx              core.JsxEmit
	DeclarationMap   bool
	CustomConditions []string
	// CompilerOptions carries the tsconfig options that change the declaration
	// output and the type checking behind it: the strict family, "lib",
	// "types", "paths", "moduleResolution", the decorator and JSX
	// settings. The declaration-only switches are always set by this package.
	CompilerOptions *core.CompilerOptions
	// BaseURL stands in for the removed tsconfig "baseUrl" option. TypeScript
	// 5 resolved bare specifiers against it, so a tsconfig that still uses it
	// keeps working.
	BaseURL resolve.BaseURLFallback
}

// declarationCompilerOptions builds the compiler options for the declaration
// program. The tsconfig options that change type checking and inference are
// honoured; the declaration-only switches are always set here.
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

	// Strictness and inference.
	compilerOpts.Strict = src.Strict
	compilerOpts.StrictNullChecks = src.StrictNullChecks
	compilerOpts.StrictFunctionTypes = src.StrictFunctionTypes
	compilerOpts.StrictBindCallApply = src.StrictBindCallApply
	compilerOpts.StrictPropertyInitialization = src.StrictPropertyInitialization
	compilerOpts.NoImplicitAny = src.NoImplicitAny
	compilerOpts.NoImplicitThis = src.NoImplicitThis
	compilerOpts.NoImplicitReturns = src.NoImplicitReturns
	compilerOpts.NoImplicitOverride = src.NoImplicitOverride
	compilerOpts.NoUncheckedIndexedAccess = src.NoUncheckedIndexedAccess
	compilerOpts.NoUncheckedSideEffectImports = src.NoUncheckedSideEffectImports
	compilerOpts.ExactOptionalPropertyTypes = src.ExactOptionalPropertyTypes
	compilerOpts.UseUnknownInCatchVariables = src.UseUnknownInCatchVariables
	compilerOpts.NoPropertyAccessFromIndexSignature = src.NoPropertyAccessFromIndexSignature
	compilerOpts.NoFallthroughCasesInSwitch = src.NoFallthroughCasesInSwitch
	compilerOpts.AllowUnreachableCode = src.AllowUnreachableCode
	compilerOpts.AllowUnusedLabels = src.AllowUnusedLabels
	compilerOpts.NoUnusedLocals = src.NoUnusedLocals
	compilerOpts.NoUnusedParameters = src.NoUnusedParameters
	compilerOpts.Lib = src.Lib
	compilerOpts.Types = src.Types
	compilerOpts.TypeRoots = src.TypeRoots
	compilerOpts.SkipDefaultLibCheck = src.SkipDefaultLibCheck

	// Module resolution.
	compilerOpts.ModuleResolution = resolve.NormalizeModuleResolution(src.ModuleResolution)
	compilerOpts.Paths = resolve.WithPathsFallback(src.Paths, opts.BaseURL, src.PathsBasePath)
	compilerOpts.PathsBasePath = src.PathsBasePath
	compilerOpts.ResolvePackageJsonExports = src.ResolvePackageJsonExports
	compilerOpts.ResolvePackageJsonImports = src.ResolvePackageJsonImports
	compilerOpts.CheckJs = src.CheckJs
	compilerOpts.ForceConsistentCasingInFileNames = src.ForceConsistentCasingInFileNames

	// Emit shape.
	compilerOpts.ExperimentalDecorators = src.ExperimentalDecorators
	compilerOpts.EmitDecoratorMetadata = src.EmitDecoratorMetadata
	compilerOpts.UseDefineForClassFields = src.UseDefineForClassFields
	compilerOpts.JsxFactory = src.JsxFactory
	compilerOpts.JsxFragmentFactory = src.JsxFragmentFactory
	compilerOpts.JsxImportSource = src.JsxImportSource
	compilerOpts.ReactNamespace = src.ReactNamespace
	compilerOpts.PreserveConstEnums = src.PreserveConstEnums
	compilerOpts.VerbatimModuleSyntax = src.VerbatimModuleSyntax
	return compilerOpts
}

// EmitProgram runs a tsgo declaration-only program for the given TypeScript paths.
// Keys in the result map are normalized via Key.
func EmitProgram(ctx context.Context, absPaths []string, opts Options) (map[string]Emit, error) {
	fs := bundled.WrapFS(osvfs.FS())
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
	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)
	program, err := compiler.NewProgram(ctx, compiler.ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(compilerOpts, roots, nil, tspath.RootedDirectoryPathFromAbsolute(modpath.DisplayRoot(absPaths)), fs.CaseSensitivity()),
		Host:           host,
		SingleThreaded: core.TSTrue,
	})
	if err != nil {
		return nil, err
	}
	if program == nil {
		return nil, fmt.Errorf("declaration program was not created")
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
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		abs = path
	}
	id := modpath.ModuleID(abs)
	if runtime.GOOS == "windows" {
		id = strings.ToLower(id)
	}
	return id
}

// StripMapComment removes sourceMappingURL comments from declaration text.
func StripMapComment(src string) string {
	return jsedit.StripSourceMappingComment(src)
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
