package bundler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/startracex/tsgo/pkg/core"
	tsmodule "github.com/startracex/tsgo/pkg/module"
	"github.com/startracex/tsgo/pkg/tspath"
	"github.com/startracex/tsgo/pkg/vfs"
	"github.com/startracex/tsgo/pkg/vfs/osvfs"
)

// nodeBuiltins are Node's core modules. They stay external so the bundle can
// import them instead of trying to find a package of the same name.
var nodeBuiltins = map[string]struct{}{
	"assert": {}, "assert/strict": {}, "async_hooks": {}, "buffer": {},
	"child_process": {}, "cluster": {}, "console": {}, "constants": {},
	"crypto": {}, "dgram": {}, "diagnostics_channel": {}, "dns": {},
	"dns/promises": {}, "domain": {}, "events": {}, "fs": {}, "fs/promises": {},
	"http": {}, "http2": {}, "https": {}, "inspector": {}, "inspector/promises": {},
	"module": {}, "net": {}, "os": {}, "path": {}, "path/posix": {}, "path/win32": {},
	"perf_hooks": {}, "process": {}, "punycode": {}, "querystring": {},
	"readline": {}, "readline/promises": {}, "repl": {}, "stream": {},
	"stream/consumers": {}, "stream/promises": {}, "stream/web": {},
	"string_decoder": {}, "timers": {}, "timers/promises": {}, "tls": {},
	"trace_events": {}, "tty": {}, "url": {}, "util": {}, "util/types": {},
	"v8": {}, "vm": {}, "wasi": {}, "worker_threads": {}, "zlib": {},
}

type resolutionHost struct {
	fs  vfs.FS
	dir tspath.RootedDirectoryPath
}

func (h resolutionHost) FS() vfs.FS { return h.fs }

func (h resolutionHost) GetCurrentDirectory() tspath.RootedDirectoryPath { return h.dir }

func newResolver(entryDir, platform string) (*tsmodule.DefaultResolver, error) {
	dir, err := filepath.Abs(entryDir)
	if err != nil {
		return nil, err
	}
	rooted := tspath.RootedDirectoryPathFromAbsolute(dir)
	return tsmodule.NewResolver(tsmodule.ResolverOptions{
		Host: resolutionHost{fs: osvfs.FS(), dir: rooted},
		CompilerOptions: &core.CompilerOptions{
			Module:                     core.ModuleKindESNext,
			ModuleResolution:           core.ModuleResolutionKindBundler,
			AllowJs:                    core.TSTrue,
			AllowImportingTsExtensions: core.TSTrue,
			NoDtsResolution:            core.TSTrue,
			ResolveJsonModule:          core.TSTrue,
			ResolvePackageJsonExports:  core.TSTrue,
			ResolvePackageJsonImports:  core.TSTrue,
			CustomConditions:           platformConditions(platform),
		},
	}), nil
}

func (b *bundler) resolve(importer, spec string) (resolved string, external bool, err error) {
	return b.resolveSpec(importer, spec, true)
}

func (b *bundler) resolveSpec(importer, spec string, allowModuleField bool) (string, bool, error) {
	if b.opts.Resolve != nil {
		path, ok, hookErr := b.opts.Resolve(importer, spec)
		if hookErr != nil {
			return "", false, hookErr
		}
		if ok {
			abs, err := absFile(path)
			if err != nil {
				return "", false, fmt.Errorf("resolve %q from %s: %w", spec, importer, err)
			}
			return abs, false, nil
		}
	}
	if err := unsupportedSpecifier(spec); err != nil {
		return "", false, err
	}
	if isExternalSpecifier(spec) || b.isUserExternal(spec) {
		b.noteExternal(spec)
		return "", true, nil
	}
	if allowModuleField && b.format == FormatESM {
		if chosen, ok, err := b.preferModuleField(importer, spec); err != nil || ok {
			return chosen, false, err
		}
	}
	return b.resolveWithCompiler(importer, spec)
}

func (b *bundler) resolveWithCompiler(importer, spec string) (string, bool, error) {
	containing, ok := tspath.TryRootedFilePathFromAbsolute(importer)
	if !ok {
		return "", false, fmt.Errorf("invalid importer path %s", importer)
	}
	resolved, _, err := b.resolver.ResolveModuleName(spec, containing, b.mode, nil)
	if err != nil {
		return "", false, err
	}
	if resolved == nil || !resolved.IsResolved() {
		if isExternalSpecifier(spec) {
			b.noteExternal(spec)
			return "", true, nil
		}
		return "", false, fmt.Errorf("cannot resolve %q imported by %s", spec, importer)
	}
	abs, err := absFile(filepath.FromSlash(resolved.ResolvedFileName.AsString()))
	if err != nil {
		return "", false, fmt.Errorf("cannot resolve %q imported by %s: %w", spec, importer, err)
	}
	if strings.HasSuffix(strings.ToLower(abs), ".d.ts") {
		return "", false, fmt.Errorf("%q imported by %s resolved to declaration file %s", spec, importer, abs)
	}
	if err := unsupportedSpecifier(abs); err != nil {
		return "", false, err
	}
	return abs, false, nil
}

func (b *bundler) preferModuleField(importer, spec string) (string, bool, error) {
	name, sub, ok := packageSpecifier(spec)
	if !ok || sub != "" {
		return "", false, nil
	}
	dir, meta, ok := findPackageMeta(importer, name)
	if !ok || meta.hasExports() || meta.Module == "" {
		return "", false, nil
	}
	rel := meta.Module
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	chosen, external, err := b.resolveSpec(filepath.Join(dir, "package.json"), filepath.ToSlash(rel), false)
	if err != nil || external || chosen == "" {
		return "", false, err
	}
	return chosen, true, nil
}

func (b *bundler) noteExternal(spec string) {
	if b.externals == nil {
		b.externals = map[string]struct{}{}
	}
	b.externals[spec] = struct{}{}
}

func (b *bundler) isUserExternal(spec string) bool {
	if b.outMode == ModeLib {
		if _, _, ok := packageSpecifier(spec); ok {
			return true
		}
	}
	name, _, ok := packageSpecifier(spec)
	if !ok {
		return false
	}
	for _, ext := range b.opts.External {
		if spec == ext || name == ext || strings.HasPrefix(spec, ext+"/") {
			return true
		}
	}
	return false
}

func platformConditions(platform string) []string {
	switch platform {
	case "browser":
		return []string{"browser"}
	case "", "node":
		return []string{"node"}
	default:
		return []string{platform}
	}
}

func isExternalSpecifier(spec string) bool {
	if strings.HasPrefix(spec, "node:") {
		return true
	}
	_, ok := nodeBuiltins[spec]
	return ok
}

func packageSpecifier(spec string) (name, sub string, ok bool) {
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "node:") {
		return "", "", false
	}
	if strings.Contains(spec, ":") {
		return "", "", false
	}
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") {
		if len(parts) < 2 || parts[1] == "" {
			return "", "", false
		}
		return parts[0] + "/" + parts[1], strings.Join(parts[2:], "/"), true
	}
	if parts[0] == "" {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], "/"), true
}

type packageMeta struct {
	Exports json.RawMessage `json:"exports"`
	Module  string          `json:"module"`
}

func (m packageMeta) hasExports() bool {
	body := bytes.TrimSpace(m.Exports)
	return len(body) > 0 && !bytes.Equal(body, []byte("null")) && !bytes.Equal(body, []byte("false"))
}

func findPackageMeta(importer, name string) (string, packageMeta, bool) {
	dir := filepath.Dir(importer)
	for {
		pkgJSON := filepath.Join(dir, "node_modules", filepath.FromSlash(name), "package.json")
		raw, err := os.ReadFile(pkgJSON)
		if err == nil {
			var meta packageMeta
			if json.Unmarshal(raw, &meta) != nil {
				return "", packageMeta{}, false
			}
			return filepath.Dir(pkgJSON), meta, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", packageMeta{}, false
		}
		dir = parent
	}
}

func unsupportedSpecifier(spec string) error {
	switch strings.ToLower(path.Ext(spec)) {
	case ".css", ".scss", ".sass", ".less":
		return fmt.Errorf("CSS imports are not supported (%s)", spec)
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".woff", ".woff2", ".ttf", ".eot":
		return fmt.Errorf("asset imports are not supported (%s)", spec)
	default:
		return nil
	}
}

func supportedSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".json":
		return true
	default:
		return false
	}
}

func absFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	return abs, nil
}

func moduleID(path string) string {
	return filepath.ToSlash(path)
}
