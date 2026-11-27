package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/output"
)

func TestPlanOutputsRightAlignFormats(t *testing.T) {
	plan, err := planOutputs(
		outputDir,
		[]string{"a", "b", "c"},
		[]bundler.Format{bundler.FormatCJS, bundler.FormatESM},
		bundler.OutputOptions{Format: bundler.FormatESM, Name: "App"},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutDir: "a", Name: "App"},
		{Format: bundler.FormatCJS, OutDir: "b", Name: "App"},
		{Format: bundler.FormatESM, OutDir: "c", Name: "App"},
	}
	if !reflect.DeepEqual(plan.Outputs, want) {
		t.Fatalf("outputs = %#v, want %#v", plan.Outputs, want)
	}
	if plan.Base != "a" {
		t.Fatalf("base = %q, want %q", plan.Base, "a")
	}
}

func TestPlanOutputsRightAlignPaths(t *testing.T) {
	plan, err := planOutputs(
		outputDir,
		[]string{"a", "b"},
		[]bundler.Format{bundler.FormatESM, bundler.FormatCJS, bundler.FormatIIFE},
		bundler.OutputOptions{Format: bundler.FormatESM, Name: "App"},
		"base",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutDir: filepath.Join("base", "esm"), Name: "App"},
		{Format: bundler.FormatCJS, OutDir: "a", Name: "App"},
		{Format: bundler.FormatIIFE, OutDir: "b", Name: "App"},
	}
	if !reflect.DeepEqual(plan.Outputs, want) {
		t.Fatalf("outputs = %#v, want %#v", plan.Outputs, want)
	}
	if plan.Base != "base" {
		t.Fatalf("base = %q, want %q", plan.Base, "base")
	}
}

func TestPlanOutputsSinglePathDerivesEachFormat(t *testing.T) {
	base := filepath.Join("dist", "index.js")
	plan, err := planOutputs(
		outputFile,
		[]string{base},
		[]bundler.Format{bundler.FormatESM, bundler.FormatCJS},
		bundler.OutputOptions{Format: bundler.FormatESM},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutFile: output.DeriveOutFile(base, string(bundler.FormatESM))},
		{Format: bundler.FormatCJS, OutFile: output.DeriveOutFile(base, string(bundler.FormatCJS))},
	}
	if !reflect.DeepEqual(plan.Outputs, want) {
		t.Fatalf("outputs = %#v, want %#v", plan.Outputs, want)
	}
	if plan.Base != base {
		t.Fatalf("base = %q, want %q", plan.Base, base)
	}
}

func TestPlanOutputsKeepsExplicitPaths(t *testing.T) {
	paths := []string{filepath.Join("dist", "a.mjs"), filepath.Join("dist", "b.cjs")}
	plan, err := planOutputs(
		outputFile,
		paths,
		[]bundler.Format{bundler.FormatESM, bundler.FormatCJS},
		bundler.OutputOptions{Format: bundler.FormatESM},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutFile: paths[0]},
		{Format: bundler.FormatCJS, OutFile: paths[1]},
	}
	if !reflect.DeepEqual(plan.Outputs, want) {
		t.Fatalf("outputs = %#v, want %#v", plan.Outputs, want)
	}
}

func TestTemplateOfHashPlaceholder(t *testing.T) {
	if got := templateOf("dist/[hash:8].js"); got != "dist/[hash:8].js" {
		t.Fatalf("templateOf valid hash = %q", got)
	}
	if got := templateOf("dist/[hash.js"); got != "" {
		t.Fatalf("templateOf invalid hash = %q", got)
	}
	if got := templateOf("dist/[path].js"); got != "dist/[path].js" {
		t.Fatalf("templateOf path = %q", got)
	}
}

func TestPlanOutputsTemplate(t *testing.T) {
	pattern := filepath.Join("dist", "[name].cjs")
	plan, err := planOutputs(outputFile, []string{pattern}, []bundler.Format{bundler.FormatCJS}, bundler.OutputOptions{Format: bundler.FormatCJS}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].NameTemplate != pattern {
		t.Fatalf("outputs = %#v, want template %q", plan.Outputs, pattern)
	}
	var opts bundler.Options
	applyOutputPlan(&opts, plan)
	if len(opts.Outputs) != 1 || opts.Outputs[0].NameTemplate != pattern {
		t.Fatalf("Outputs = %#v, want template %q", opts.Outputs, pattern)
	}
}

func TestPlanOutputsScalarAndDerivedFromBase(t *testing.T) {
	scalar, err := planOutputs(outputDir, []string{"dist"}, []bundler.Format{bundler.FormatESM}, bundler.OutputOptions{Format: bundler.FormatESM}, "tsout")
	if err != nil {
		t.Fatal(err)
	}
	if len(scalar.Outputs) != 1 || scalar.Outputs[0].Format != bundler.FormatESM || scalar.Outputs[0].OutDir != "dist" || scalar.Base != "dist" {
		t.Fatalf("scalar plan = %#v", scalar)
	}

	derived, err := planOutputs(outputDir, nil, []bundler.Format{bundler.FormatESM, bundler.FormatCJS}, bundler.OutputOptions{Format: bundler.FormatESM}, "dist")
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutDir: filepath.Join("dist", "esm")},
		{Format: bundler.FormatCJS, OutDir: filepath.Join("dist", "cjs")},
	}
	if !reflect.DeepEqual(derived.Outputs, want) {
		t.Fatalf("outputs = %#v, want %#v", derived.Outputs, want)
	}
}
