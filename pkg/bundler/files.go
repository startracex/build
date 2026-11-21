package bundler

import (
	"encoding/json"
	"fmt"
	"os"
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
	if o.Declaration == nil {
		return true
	}
	return *o.Declaration
}

func (o Options) sourceMap() bool {
	if o.SourceMap == nil {
		return true
	}
	return *o.SourceMap
}

func isJavaScriptOutput(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs":
		return !strings.HasSuffix(strings.ToLower(path), ".d.ts")
	default:
		return false
	}
}

func isTypeScriptPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts":
		return !strings.HasSuffix(strings.ToLower(path), ".d.ts")
	default:
		return false
	}
}

func (b *bundler) declarationText(path, src string) (string, error) {
	out := transpile.TranspileDeclaration(b.ctx, src, transpile.Options{
		FileName: filepath.Base(path),
		CompilerOptions: &core.CompilerOptions{
			Module: core.ModuleKindESNext,
			Target: core.ScriptTargetES2022,
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
		files = append(files, Output{Path: jsName + ".map", Data: appSourceMap(js, filepath.Base(jsName))})
	}
	return &Result{Files: files, entry: js}, nil
}

func stripSourceMappingComment(src string) string {
	lines := strings.Split(src, "\n")
	var kept []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//# sourceMappingURL=") || strings.HasPrefix(trim, "//@ sourceMappingURL=") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func packageEffectsFree(file string) bool {
	dir := filepath.Dir(file)
	for {
		raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var meta struct {
				SideEffects any `json:"sideEffects"`
			}
			if json.Unmarshal(raw, &meta) == nil {
				flag, ok := meta.SideEffects.(bool)
				return ok && !flag
			}
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func javascriptOutputAbs(abs string) string {
	ext := strings.ToLower(filepath.Ext(abs))
	switch ext {
	case ".ts", ".tsx", ".mts", ".cts", ".json":
		return strings.TrimSuffix(abs, filepath.Ext(abs)) + ".js"
	default:
		return abs
	}
}
