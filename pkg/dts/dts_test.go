package dts

import (
	"testing"

	"github.com/startracex/tsgo/pkg/core"
)

func TestDeclarationCompilerOptionsKeepsTSConfigSettings(t *testing.T) {
	src := &core.CompilerOptions{
		Strict:                     core.TSTrue,
		StrictNullChecks:           core.TSTrue,
		ExactOptionalPropertyTypes: core.TSTrue,
		NoImplicitAny:              core.TSTrue,
		ExperimentalDecorators:     core.TSTrue,
		EmitDecoratorMetadata:      core.TSTrue,
		UseDefineForClassFields:    core.TSFalse,
		JsxFactory:                 "h",
		JsxFragmentFactory:         "Fragment",
		JsxImportSource:            "preact",
		ModuleResolution:           core.ModuleResolutionKindNode16,
		PathsBasePath:              "/project",
		Lib:                        []string{"es2020"},
		Types:                      []string{"node"},
	}
	opts := declarationCompilerOptions(Options{CompilerOptions: src, Target: core.ScriptTargetES2020}, core.TSTrue)

	if opts.Strict != core.TSTrue || opts.StrictNullChecks != core.TSTrue {
		t.Fatalf("strict options not copied")
	}
	if opts.ExactOptionalPropertyTypes != core.TSTrue || opts.NoImplicitAny != core.TSTrue {
		t.Fatalf("strict options not copied")
	}
	if opts.ExperimentalDecorators != core.TSTrue || opts.EmitDecoratorMetadata != core.TSTrue {
		t.Fatalf("decorator options not copied")
	}
	if opts.UseDefineForClassFields != core.TSFalse {
		t.Fatalf("useDefineForClassFields not copied")
	}
	if opts.JsxFactory != "h" || opts.JsxFragmentFactory != "Fragment" || opts.JsxImportSource != "preact" {
		t.Fatalf("jsx options not copied")
	}
	if opts.ModuleResolution != core.ModuleResolutionKindNode16 {
		t.Fatalf("moduleResolution not copied")
	}
	if opts.PathsBasePath != "/project" {
		t.Fatalf("pathsBasePath not copied")
	}
	if len(opts.Lib) != 1 || opts.Lib[0] != "es2020" {
		t.Fatalf("lib not copied")
	}
	if len(opts.Types) != 1 || opts.Types[0] != "node" {
		t.Fatalf("types not copied")
	}
	if opts.Declaration != core.TSTrue || opts.EmitDeclarationOnly != core.TSTrue {
		t.Fatalf("declaration switches not applied")
	}
	if opts.IsolatedDeclarations != core.TSFalse || opts.NoCheck != core.TSFalse {
		t.Fatalf("declaration switches not applied")
	}
}

func TestDeclarationCompilerOptionsDefaults(t *testing.T) {
	opts := declarationCompilerOptions(Options{}, core.TSFalse)
	if opts.Declaration != core.TSTrue || opts.EmitDeclarationOnly != core.TSTrue {
		t.Fatalf("declaration switches not applied")
	}
	if opts.ModuleResolution != core.ModuleResolutionKindBundler {
		t.Fatalf("moduleResolution default = %v", opts.ModuleResolution)
	}
	if opts.Strict != core.TSUnknown || opts.ExperimentalDecorators != core.TSUnknown {
		t.Fatalf("unexpected defaults")
	}
}
