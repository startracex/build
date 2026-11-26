package compileropts

import (
	"testing"

	"github.com/startracex/build/pkg/resolve"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tspath"
)

// baseSource returns fresh options; CompilerOptions has a noCopy marker, so tests never copy it.
func baseSource() *core.CompilerOptions {
	return &core.CompilerOptions{JsxFactory: "h"}
}

func TestEmitKeyTracksSwitches(t *testing.T) {
	want := EmitFrom(baseSource()).Key()
	mutants := map[string]func(*core.CompilerOptions){
		"ExperimentalDecorators":  func(o *core.CompilerOptions) { o.ExperimentalDecorators = core.TSTrue },
		"EmitDecoratorMetadata":   func(o *core.CompilerOptions) { o.EmitDecoratorMetadata = core.TSTrue },
		"UseDefineForClassFields": func(o *core.CompilerOptions) { o.UseDefineForClassFields = core.TSTrue },
		"VerbatimModuleSyntax":    func(o *core.CompilerOptions) { o.VerbatimModuleSyntax = core.TSTrue },
		"IsolatedModules":         func(o *core.CompilerOptions) { o.IsolatedModules = core.TSTrue },
		"ImportHelpers":           func(o *core.CompilerOptions) { o.ImportHelpers = core.TSTrue },
		"NoEmitHelpers":           func(o *core.CompilerOptions) { o.NoEmitHelpers = core.TSTrue },
		"JsxFactory":              func(o *core.CompilerOptions) { o.JsxFactory = "createElement" },
		"JsxFragmentFactory":      func(o *core.CompilerOptions) { o.JsxFragmentFactory = "Fragment" },
		"JsxImportSource":         func(o *core.CompilerOptions) { o.JsxImportSource = "react" },
		"ReactNamespace":          func(o *core.CompilerOptions) { o.ReactNamespace = "React" },
		"PreserveConstEnums":      func(o *core.CompilerOptions) { o.PreserveConstEnums = core.TSTrue },
		"RemoveComments":          func(o *core.CompilerOptions) { o.RemoveComments = core.TSTrue },
		"NewLine":                 func(o *core.CompilerOptions) { o.NewLine = core.NewLineKindCRLF },
	}
	for name, mutate := range mutants {
		opts := baseSource()
		mutate(opts)
		if got := EmitFrom(opts).Key(); got == want {
			t.Errorf("%s did not change the cache key", name)
		}
	}
}

// TestEmitKeyIgnoresTypeCheckOptions checks that type-only options do not invalidate the cache.
func TestEmitKeyIgnoresTypeCheckOptions(t *testing.T) {
	want := EmitFrom(baseSource()).Key()
	opts := baseSource()
	opts.Strict = core.TSTrue
	opts.Lib = []string{"es2022"}
	opts.OutDir = "/tmp/out"
	if got := EmitFrom(opts).Key(); got != want {
		t.Fatalf("type-check-only options changed the cache key: %q != %q", got, want)
	}
}

// TestEmitKeyQuotesValues checks that a separator inside a value cannot collide with another field, as the old hand-rolled format did.
func TestEmitKeyQuotesValues(t *testing.T) {
	a := &core.CompilerOptions{JsxFactory: "x;jff="}
	b := &core.CompilerOptions{JsxFactory: "x", JsxFragmentFactory: ";jff="}
	if EmitFrom(a).Key() == EmitFrom(b).Key() {
		t.Fatal("different emit options produced the same cache key")
	}
}

// TestEmitApplyCopiesEverySwitch checks that apply copies every switch and leaves NewLine alone when unset.
func TestEmitApplyCopiesEverySwitch(t *testing.T) {
	src := &core.CompilerOptions{
		ExperimentalDecorators:  core.TSTrue,
		EmitDecoratorMetadata:   core.TSTrue,
		UseDefineForClassFields: core.TSFalse,
		VerbatimModuleSyntax:    core.TSTrue,
		IsolatedModules:         core.TSTrue,
		ImportHelpers:           core.TSTrue,
		NoEmitHelpers:           core.TSTrue,
		JsxFactory:              "h",
		JsxFragmentFactory:      "F",
		JsxImportSource:         "preact",
		ReactNamespace:          "R",
		PreserveConstEnums:      core.TSTrue,
		RemoveComments:          core.TSTrue,
		NewLine:                 core.NewLineKindLF,
	}
	var dst core.CompilerOptions
	EmitFrom(src).Apply(&dst)
	if dst.ExperimentalDecorators != core.TSTrue || dst.EmitDecoratorMetadata != core.TSTrue ||
		dst.UseDefineForClassFields != core.TSFalse || dst.VerbatimModuleSyntax != core.TSTrue ||
		dst.IsolatedModules != core.TSTrue || dst.ImportHelpers != core.TSTrue ||
		dst.NoEmitHelpers != core.TSTrue || dst.JsxFactory != "h" ||
		dst.JsxFragmentFactory != "F" || dst.JsxImportSource != "preact" ||
		dst.ReactNamespace != "R" || dst.PreserveConstEnums != core.TSTrue ||
		dst.RemoveComments != core.TSTrue || dst.NewLine != core.NewLineKindLF {
		t.Fatal("apply lost a switch")
	}
	var none core.CompilerOptions
	EmitFrom(&core.CompilerOptions{}).Apply(&none)
	if none.NewLine != core.NewLineKindNone {
		t.Fatalf("apply changed the default line ending: %v", none.NewLine)
	}
}

// TestTranspileKeyTracksBundlerSwitches checks that bundler-derived per-emit options are in the cache key; otherwise ESM and CJS sharing a cache dir would reuse each other's text.
func TestTranspileKeyTracksBundlerSwitches(t *testing.T) {
	base := Transpile{
		Module:    core.ModuleKindESNext,
		Target:    core.ScriptTargetES2022,
		Jsx:       core.JsxEmitReactJSX,
		SourceMap: core.TSFalse,
		Emit:      EmitFrom(baseSource()),
	}
	want := base.Key()
	mutants := map[string]func(*Transpile){
		"Module":    func(o *Transpile) { o.Module = core.ModuleKindCommonJS },
		"Target":    func(o *Transpile) { o.Target = core.ScriptTargetES2015 },
		"Jsx":       func(o *Transpile) { o.Jsx = core.JsxEmitNone },
		"SourceMap": func(o *Transpile) { o.SourceMap = core.TSTrue },
		"Emit":      func(o *Transpile) { o.Emit.Syntax.JsxFactory = "createElement" },
	}
	for name, mutate := range mutants {
		opts := base
		mutate(&opts)
		if got := opts.Key(); got == want {
			t.Errorf("%s did not change the cache key", name)
		}
	}
}

// TestTranspileCompilerOptions checks that the cache-key value also fills the compiler options.
func TestTranspileCompilerOptions(t *testing.T) {
	opts := Transpile{
		Module:    core.ModuleKindCommonJS,
		Target:    core.ScriptTargetES2022,
		Jsx:       core.JsxEmitReactJSX,
		SourceMap: core.TSTrue,
		Emit:      EmitFrom(baseSource()),
	}
	got := opts.CompilerOptions()
	if got.Module != core.ModuleKindCommonJS || got.Target != core.ScriptTargetES2022 ||
		got.Jsx != core.JsxEmitReactJSX || got.SourceMap != core.TSTrue || got.JsxFactory != "h" {
		t.Fatal("compiler options lost a field")
	}
}

// TestSyntaxApplySkipsJavaScriptOnlySwitches checks that declaration emit applies Syntax but not JavaScript-only text switches.
func TestSyntaxApplySkipsJavaScriptOnlySwitches(t *testing.T) {
	src := &core.CompilerOptions{
		JsxFactory:      "h",
		RemoveComments:  core.TSTrue,
		NewLine:         core.NewLineKindLF,
		IsolatedModules: core.TSTrue,
		ImportHelpers:   core.TSTrue,
		NoEmitHelpers:   core.TSTrue,
	}
	var dst core.CompilerOptions
	SyntaxFrom(src).Apply(&dst)
	if dst.JsxFactory != "h" {
		t.Fatalf("Syntax apply lost a switch: %q", dst.JsxFactory)
	}
	if dst.RemoveComments != core.TSUnknown || dst.NewLine != core.NewLineKindNone ||
		dst.IsolatedModules != core.TSUnknown || dst.ImportHelpers != core.TSUnknown ||
		dst.NoEmitHelpers != core.TSUnknown {
		t.Fatal("Syntax apply copied a JavaScript-only switch")
	}
}

// TestTypesApplyCopiesTypeCheckOptions checks that the projection copies strict and library settings, and nothing else.
func TestTypesApplyCopiesTypeCheckOptions(t *testing.T) {
	src := &core.CompilerOptions{
		Strict:                   core.TSTrue,
		NoImplicitAny:            core.TSFalse,
		NoUncheckedIndexedAccess: core.TSTrue,
		Lib:                      []string{"es2022"},
		Types:                    []string{"node"},
		SkipDefaultLibCheck:      core.TSTrue,
		JsxFactory:               "h",
	}
	var dst core.CompilerOptions
	TypesFrom(src).Apply(&dst)
	if dst.Strict != core.TSTrue || dst.NoImplicitAny != core.TSFalse ||
		dst.NoUncheckedIndexedAccess != core.TSTrue || len(dst.Lib) != 1 ||
		len(dst.Types) != 1 || dst.SkipDefaultLibCheck != core.TSTrue {
		t.Fatal("Types apply lost a switch")
	}
	if dst.JsxFactory != "" || dst.RemoveComments != core.TSUnknown {
		t.Fatal("Types apply touched an emit switch")
	}
}

// TestResolutionApply checks that the projection normalizes the resolution kind and keeps the baseUrl fallback.
func TestResolutionApply(t *testing.T) {
	src := &core.CompilerOptions{
		ModuleResolution: core.ModuleResolutionKindClassic,
		CheckJs:          core.TSTrue,
	}
	baseURL := resolve.BaseURLFallback{Directory: tspath.RootedDirectoryPath("/repo")}
	var dst core.CompilerOptions
	ResolutionFrom(src, baseURL).Apply(&dst)
	if dst.ModuleResolution != core.ModuleResolutionKindBundler {
		t.Fatalf("module resolution was not normalized: %v", dst.ModuleResolution)
	}
	if dst.Paths == nil || !dst.Paths.Has("*") {
		t.Fatal("baseUrl fallback mapping was not applied")
	}
	if dst.CheckJs != core.TSTrue {
		t.Fatal("Resolution apply lost checkJs")
	}
	if dst.JsxFactory != "" || dst.Strict != core.TSUnknown {
		t.Fatal("Resolution apply touched another kind of switch")
	}
}
