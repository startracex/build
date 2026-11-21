package bundler

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/startracex/tsgo/pkg/bundled"
	"github.com/startracex/tsgo/pkg/compiler"
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tsoptions"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
)

type declEmit struct {
	text      string
	sourceMap string
}

// attachDeclarations fills library units from a tsgo program emit so inferred
// types come from the checker. A file the program does not emit falls back to
// single-file declaration transpile.
func (b *bundler) attachDeclarations() error {
	if !b.decl {
		return nil
	}
	checked, err := b.emitProgramDeclarations()
	if err != nil {
		return err
	}
	for id, unit := range b.units {
		abs := filepath.FromSlash(id)
		if got, ok := checked[declKey(abs)]; ok && strings.TrimSpace(got.text) != "" {
			unit.dts = stripSourceMappingComment(got.text)
			unit.dtsMap = got.sourceMap
			continue
		}
		if !isTypeScriptPath(abs) || strings.TrimSpace(unit.dts) != "" {
			continue
		}
		text, err := b.declarationText(abs, unit.source)
		if err != nil {
			return err
		}
		unit.dts = text
	}
	return nil
}

func (b *bundler) emitProgramDeclarations() (map[string]declEmit, error) {
	fs := bundled.WrapFS(osvfs.FS())
	var absPaths []string
	for id := range b.units {
		abs := filepath.FromSlash(id)
		if isTypeScriptPath(abs) {
			absPaths = append(absPaths, abs)
		}
	}
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
		return map[string]declEmit{}, nil
	}

	opts := &core.CompilerOptions{
		Declaration:          core.TSTrue,
		DeclarationMap:       core.TSTrue,
		EmitDeclarationOnly:  core.TSTrue,
		IsolatedDeclarations: core.TSFalse,
		NoCheck:              core.TSFalse,
		SkipLibCheck:         core.TSTrue,
		AllowJs:              core.TSTrue,
		ResolveJsonModule:    core.TSTrue,
		Module:               core.ModuleKindESNext,
		ModuleResolution:     core.ModuleResolutionKindBundler,
		Target:               core.ScriptTargetESNext,
		Jsx:                  b.jsx,
		CustomConditions:     platformConditions(b.opts.Platform),
	}
	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)
	program, err := compiler.NewProgram(b.ctx, compiler.ProgramOptions{
		Config:         tsoptions.NewParsedCommandLine(opts, roots, nil, tspath.RootedDirectoryPathFromAbsolute(displayRoot(absPaths)), fs.CaseSensitivity()),
		Host:           host,
		SingleThreaded: core.TSTrue,
	})
	if err != nil {
		return nil, err
	}
	if program == nil {
		return nil, fmt.Errorf("declaration program was not created")
	}

	out := map[string]declEmit{}
	result := program.Emit(b.ctx, compiler.EmitOptions{
		EmitOnly:  compiler.EmitOnlyDts,
		ForceEmit: true,
		WriteFile: func(fileName tspath.RootedFilePath, text string, data *compiler.WriteFileData) error {
			if data == nil || data.SourceFile == nil {
				return nil
			}
			key := declKey(data.SourceFile.FileName().AsString())
			slot := out[key]
			if strings.HasSuffix(strings.ToLower(fileName.AsString()), ".map") {
				slot.sourceMap = text
			} else {
				slot.text = text
			}
			out[key] = slot
			return nil
		},
	})
	if result == nil {
		return nil, fmt.Errorf("declaration emit canceled")
	}
	if len(out) == 0 {
		if err := errorFromDiagnostics("declaration", "", result.Diagnostics); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("declaration emit produced no files")
	}
	return out, nil
}

func declKey(path string) string {
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		abs = path
	}
	id := moduleID(abs)
	if runtime.GOOS == "windows" {
		id = strings.ToLower(id)
	}
	return id
}
