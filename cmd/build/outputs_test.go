package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/startracex/build/pkg/bundler"
)

func TestPlanOutputsRightAlignFormats(t *testing.T) {
	plan, err := planOutputs(
		outputDir,
		[]string{"a", "b", "c"},
		[]bundler.Format{bundler.FormatCJS, bundler.FormatESM},
		bundler.FormatESM,
		"",
		"App",
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
		bundler.FormatESM,
		"base",
		"App",
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
		bundler.FormatESM,
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bundler.OutputOptions{
		{Format: bundler.FormatESM, OutFile: bundler.DeriveOutFile(base, bundler.FormatESM)},
		{Format: bundler.FormatCJS, OutFile: bundler.DeriveOutFile(base, bundler.FormatCJS)},
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
		bundler.FormatESM,
		"",
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

func TestPlanOutputsTemplate(t *testing.T) {
	pattern := filepath.Join("dist", "[name].cjs")
	plan, err := planOutputs(outputFile, []string{pattern}, []bundler.Format{bundler.FormatCJS}, bundler.FormatCJS, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Template != pattern {
		t.Fatalf("template = %q, want %q", plan.Template, pattern)
	}
	var opts bundler.Options
	applyOutputPlan(&opts, plan)
	if opts.NameTemplate != pattern {
		t.Fatalf("NameTemplate = %q, want %q", opts.NameTemplate, pattern)
	}
}

func TestPlanOutputsScalarAndDerivedFromBase(t *testing.T) {
	scalar, err := planOutputs(outputDir, []string{"dist"}, []bundler.Format{bundler.FormatESM}, bundler.FormatESM, "tsout", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(scalar.Outputs) != 0 || scalar.Base != "dist" || scalar.Format != bundler.FormatESM {
		t.Fatalf("scalar plan = %#v", scalar)
	}

	derived, err := planOutputs(outputDir, nil, []bundler.Format{bundler.FormatESM, bundler.FormatCJS}, bundler.FormatESM, "dist", "")
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
