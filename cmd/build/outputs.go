package main

import (
	"fmt"
	"strings"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/output"
)

type outputMode string

const (
	outputFile outputMode = "file"
	outputDir  outputMode = "dir"
)

// outputPlan describes CLI emit targets; when Outputs is empty, Base and Format define one scalar output.
type outputPlan struct {
	Mode     outputMode
	Base     string
	Format   bundler.Format
	Template string
	Outputs  []bundler.OutputOptions
}

func templateOf(path string) string {
	if strings.Contains(path, "[name]") || strings.Contains(path, "[path]") || strings.Contains(path, "[format]") || output.HasHashPlaceholder(path) {
		return path
	}
	return ""
}

// planOutputs pairs paths with formats from the right, filling gaps with fallbackFormat.
// Missing paths are derived from fallbackBase or the first explicit path.
func planOutputs(mode outputMode, paths []string, formats []bundler.Format, fallbackFormat bundler.Format, fallbackBase, name string) (outputPlan, error) {
	if mode != outputFile && mode != outputDir {
		return outputPlan{}, fmt.Errorf("unsupported output mode %q", mode)
	}
	if len(formats) == 0 {
		formats = []bundler.Format{fallbackFormat}
	}
	if fallbackFormat == "" {
		fallbackFormat = bundler.FormatESM
	}

	if len(paths) == 0 {
		return derivedPlan(mode, fallbackBase, formats, name), nil
	}

	// One explicit path with several formats is a derivation base, not a literal output path.
	if len(paths) == 1 && len(formats) > 1 {
		return derivedPlan(mode, paths[0], formats, name), nil
	}

	target := max(len(paths), len(formats))
	alignedFormats := make([]bundler.Format, target)
	formatOffset := target - len(formats)
	for i := range alignedFormats {
		if i < formatOffset {
			alignedFormats[i] = fallbackFormat
			continue
		}
		alignedFormats[i] = formats[i-formatOffset]
	}

	base := fallbackBase
	if base == "" {
		base = paths[0]
	}
	pathOffset := target - len(paths)
	alignedPaths := make([]string, target)
	for i := range alignedPaths {
		if i < pathOffset {
			alignedPaths[i] = derivePath(mode, base, alignedFormats[i])
			continue
		}
		alignedPaths[i] = paths[i-pathOffset]
	}

	if target == 1 {
		return outputPlan{Mode: mode, Base: alignedPaths[0], Format: alignedFormats[0], Template: templateOf(alignedPaths[0])}, nil
	}

	outs := make([]bundler.OutputOptions, 0, target)
	for i := range target {
		out := bundler.OutputOptions{Format: alignedFormats[i], Name: name}
		if mode == outputFile {
			out.OutFile = alignedPaths[i]
			out.NameTemplate = templateOf(alignedPaths[i])
		} else {
			out.OutDir = alignedPaths[i]
		}
		outs = append(outs, out)
	}
	return outputPlan{Mode: mode, Base: base, Outputs: outs}, nil
}

func derivedPlan(mode outputMode, base string, formats []bundler.Format, name string) outputPlan {
	if len(formats) == 1 {
		return outputPlan{Mode: mode, Base: base, Format: formats[0], Template: templateOf(base)}
	}
	outs := make([]bundler.OutputOptions, 0, len(formats))
	for _, format := range formats {
		out := bundler.OutputOptions{Format: format, Name: name}
		if mode == outputFile {
			out.OutFile = derivePath(mode, base, format)
			out.NameTemplate = templateOf(out.OutFile)
		} else {
			out.OutDir = derivePath(mode, base, format)
		}
		outs = append(outs, out)
	}
	return outputPlan{Mode: mode, Base: base, Outputs: outs}
}

func derivePath(mode outputMode, base string, format bundler.Format) string {
	if mode == outputFile {
		return output.DeriveOutFile(base, string(format))
	}
	return output.DeriveOutDir(base, string(format))
}

func applyOutputPlan(opts *bundler.Options, plan outputPlan) {
	opts.Outputs = plan.Outputs
	opts.NameTemplate = plan.Template
	opts.OutFile = ""
	opts.OutDir = ""
	if plan.Mode == outputFile {
		opts.OutFile = plan.Base
	} else {
		opts.OutDir = plan.Base
	}
	if len(plan.Outputs) == 0 {
		opts.Format = plan.Format
	}
}
