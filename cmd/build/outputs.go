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

// outputPlan describes CLI emit targets. Outputs always holds concrete targets.
type outputPlan struct {
	Mode    outputMode
	Base    string
	Outputs []bundler.OutputOptions
}

func templateOf(path string) string {
	if strings.Contains(path, "[name]") || strings.Contains(path, "[path]") || strings.Contains(path, "[format]") || output.HasHashPlaceholder(path) {
		return path
	}
	return ""
}

func outputTarget(out bundler.OutputOptions, mode outputMode, format bundler.Format, target string) bundler.OutputOptions {
	out.Format = format
	out.OutFile = ""
	out.OutDir = ""
	out.NameTemplate = ""
	if mode == outputFile {
		out.OutFile = target
		out.NameTemplate = templateOf(target)
	} else {
		out.OutDir = target
	}
	return out
}

// planOutputs pairs paths with formats from the right, filling gaps with the fallback output.
func planOutputs(mode outputMode, paths []string, formats []bundler.Format, fallback bundler.OutputOptions, fallbackBase string) (outputPlan, error) {
	if mode != outputFile && mode != outputDir {
		return outputPlan{}, fmt.Errorf("unsupported output mode %q", mode)
	}
	fallbackFormat := fallback.Format
	if fallbackFormat == "" {
		fallbackFormat = bundler.FormatESM
	}
	fallback.Format = fallbackFormat
	if len(formats) == 0 {
		formats = []bundler.Format{fallbackFormat}
	}

	if len(paths) == 0 {
		return derivedPlan(mode, fallbackBase, formats, fallback), nil
	}
	// One explicit path with several formats is a derivation base, not a literal output path.
	if len(paths) == 1 && len(formats) > 1 {
		return derivedPlan(mode, paths[0], formats, fallback), nil
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
	if len(paths) >= len(formats) || base == "" {
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

	outs := make([]bundler.OutputOptions, target)
	for i := range outs {
		outs[i] = outputTarget(fallback, mode, alignedFormats[i], alignedPaths[i])
	}
	return outputPlan{Mode: mode, Base: base, Outputs: outs}, nil
}

func derivedPlan(mode outputMode, base string, formats []bundler.Format, fallback bundler.OutputOptions) outputPlan {
	outs := make([]bundler.OutputOptions, 0, len(formats))
	for _, format := range formats {
		target := base
		if len(formats) > 1 {
			target = derivePath(mode, base, format)
		}
		outs = append(outs, outputTarget(fallback, mode, format, target))
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
}
