package bundler

import (
	"fmt"
	"strings"

	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/build/pkg/output"
	"github.com/startracex/build/pkg/sourcemap"
	"github.com/startracex/build/pkg/wrap"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/transpile"
)

func normalizeMode(mode Mode) (Mode, error) {
	switch mode {
	case ModeApp:
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
	opts := b.transpileCompilerOptions(core.ModuleKindESNext, core.TSFalse)
	// A declaration file carries no implementation, so it never needs a helper.
	opts.ImportHelpers = core.TSFalse
	opts.NoEmitHelpers = core.TSFalse
	out := transpile.TranspileDeclaration(b.ctx, src, transpile.Options{
		FileName:          filepath.Base(path),
		CompilerOptions:   opts,
		ReportDiagnostics: true,
	})
	if out == nil {
		return "", fmt.Errorf("%s: declaration emit canceled", path)
	}
	text := jsedit.StripSourceMappingComment(out.OutputText)
	if strings.TrimSpace(text) == "" {
		if modpath.IsJavaScriptSource(path) {
			if err := errorFromSyntaxDiagnostics(path, src, out.Diagnostics); err != nil {
				return "", err
			}
		} else if err := errorFromDiagnostics(path, src, out.Diagnostics); err != nil {
			return "", err
		}
		return "", nil
	}
	return text, nil
}

func (b *bundler) appResult(entry, js, linked string, emitFmt Format) (*Result, error) {
	jsName := b.opts.OutFile
	if jsName == "" {
		jsName = "bundle.js"
	}
	if b.opts.NameTemplate != "" {
		name := modpath.BaseStem(entry)
		hash := ""
		if output.HasHashPlaceholder(b.opts.NameTemplate) {
			hash = output.ContentHash(js)
		}
		ext := modpath.Ext(jsName)
		if ext == "" {
			ext = ".js"
		}
		jsName = output.RenderFileName(b.opts.NameTemplate, output.FileNameParts{
			Name:   name,
			Path:   name,
			Ext:    ext,
			Type:   "entry",
			Format: string(emitFmt),
			Hash:   hash,
		})
	}
	b.sources[filepath.ToSlash(filepath.FromSlash(entry))] = jsName
	if b.sourceMap {
		js = strings.TrimRight(js, "\n") + "\n//# sourceMappingURL=" + filepath.Base(jsName) + ".map\n"
	}
	files := []Output{{Path: jsName, Data: js}}
	if b.sourceMap {
		linkSrc := linked
		if linkSrc == "" {
			linkSrc = js
		}
		mapData := b.composeAppSourceMap(linkSrc, filepath.Base(jsName))
		if emitFmt == FormatIIFE || emitFmt == FormatUMD {
			if n := sourcemap.LinesBeforeSourceComment(js); n > 0 {
				mapData = sourcemap.OffsetLines(mapData, n)
			}
			if indent := wrap.BodyIndentCols(string(emitFmt)); indent > 0 {
				mapData = sourcemap.OffsetColumns(mapData, indent)
			}
		}
		files = append(files, Output{Path: jsName + ".map", Data: mapData})
	}
	return &Result{Files: files, entry: js}, nil
}
