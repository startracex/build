package bundler

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/transpile"
)

func normalizeMode(mode Mode) (Mode, error) {
	switch mode {
	case "", ModeApp:
		return ModeApp, nil
	case ModeLib:
		return ModeLib, nil
	default:
		return "", fmt.Errorf("unsupported mode %q (use %s or %s)", mode, ModeApp, ModeLib)
	}
}

func (o Options) declaration() bool {
	return o.Declaration != nil && *o.Declaration
}

func (o Options) declarationMap() bool {
	return o.declaration() && o.DeclarationMap != nil && *o.DeclarationMap
}

func (o Options) sourceMap() bool {
	return o.SourceMap != nil && *o.SourceMap
}

func (o Options) emitTarget() core.ScriptTarget {
	if o.Target != core.ScriptTargetNone {
		return o.Target
	}
	return core.ScriptTargetESNext
}

func (b *bundler) declarationText(path, src string) (string, error) {
	out := transpile.TranspileDeclaration(b.ctx, src, transpile.Options{
		FileName: filepath.Base(path),
		CompilerOptions: &core.CompilerOptions{
			Module: core.ModuleKindESNext,
			Target: b.target,
			Jsx:    b.jsx,
		},
		ReportDiagnostics: true,
	})
	if out == nil {
		return "", fmt.Errorf("%s: declaration emit canceled", path)
	}
	text := stripSourceMappingComment(out.OutputText)
	if strings.TrimSpace(text) == "" {
		if err := errorFromDiagnostics(path, src, out.Diagnostics); err != nil {
			return "", err
		}
		return "", nil
	}
	return text, nil
}

func (b *bundler) appResult(entry, js string) (*Result, error) {
	jsName := b.opts.OutFile
	if jsName == "" {
		jsName = "bundle.js"
	}
	if b.sourceMap {
		js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + filepath.Base(jsName) + ".map\n"
	}
	files := []Output{{Path: jsName, Data: js}}
	if b.sourceMap {
		mapData := b.composeAppSourceMap(js, filepath.Base(jsName))
		// Account for wrap preamble lines when format wrapped the body.
		if b.format == FormatIIFE || b.format == FormatUMD {
			// compose already sees wrapped js; no extra offset needed.
		}
		files = append(files, Output{Path: jsName + ".map", Data: mapData})
	}
	return &Result{Files: files, entry: js}, nil
}
