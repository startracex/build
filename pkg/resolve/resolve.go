package resolve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/modpath"
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

// Config holds resolver options supplied by the bundler.
type Config struct {
	Alias       map[string]string
	External    []string
	Format      string // "esm", "cjs", ...
	LibBuild    bool   // library mode: bare package imports are external
	ResolveHook func(importer, spec string) (path string, ok bool, err error)
	OnAsset     func(abs string, q asset.Query) error
}

// Resolver resolves import specifiers using tsgo's bundler resolver.
type Resolver struct {
	res       *tsmodule.DefaultResolver
	mode      core.ResolutionMode
	cfg       Config
	externals map[string]struct{}
}

type resolutionHost struct {
	fs  vfs.FS
	dir tspath.RootedDirectoryPath
}

func (h resolutionHost) FS() vfs.FS { return h.fs }

func (h resolutionHost) GetCurrentDirectory() tspath.RootedDirectoryPath { return h.dir }

// New constructs a Resolver rooted at entryDir.
func New(entryDir string, conditions []string, mode core.ResolutionMode, cfg Config) (*Resolver, error) {
	dir, err := filepath.Abs(entryDir)
	if err != nil {
		return nil, err
	}
	rooted := tspath.RootedDirectoryPathFromAbsolute(dir)
	res := tsmodule.NewResolver(tsmodule.ResolverOptions{
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
			CustomConditions:           ExportConditions(conditions),
		},
	})
	return &Resolver{
		res:       res,
		mode:      mode,
		cfg:       cfg,
		externals: map[string]struct{}{},
	}, nil
}

// Resolve resolves spec imported from importer to an absolute file path.
func (r *Resolver) Resolve(importer, spec string) (resolved string, external bool, err error) {
	return r.resolveSpec(importer, spec, true)
}

func (r *Resolver) resolveSpec(importer, spec string, allowModuleField bool) (string, bool, error) {
	iq := asset.ParseQuery(spec)
	specPath := iq.Path
	if aliased, ok := applyAlias(r.cfg.Alias, specPath); ok {
		specPath = aliased
		if strings.HasPrefix(specPath, ".") || filepath.IsAbs(specPath) {
			joined := specPath
			if !filepath.IsAbs(joined) {
				joined = filepath.Join(filepath.Dir(importer), filepath.FromSlash(specPath))
			}
			abs, err := modpath.AbsFile(joined)
			if err == nil {
				if err := r.maybeOnAsset(abs, iq); err != nil {
					return "", false, err
				}
				return abs, false, nil
			}
		}
	}
	if r.cfg.ResolveHook != nil {
		path, ok, hookErr := r.cfg.ResolveHook(importer, specPath)
		if hookErr != nil {
			return "", false, hookErr
		}
		if ok {
			abs, err := modpath.AbsFile(path)
			if err != nil {
				return "", false, fmt.Errorf("resolve %q from %s: %w", spec, importer, err)
			}
			if err := r.maybeOnAsset(abs, iq); err != nil {
				return "", false, err
			}
			return abs, false, nil
		}
	}
	if err := unsupportedSpecifier(specPath); err != nil {
		return "", false, err
	}
	if isExternalSpecifier(specPath) || r.IsUserExternal(specPath) {
		r.NoteExternal(specPath)
		return "", true, nil
	}
	if strings.HasPrefix(specPath, ".") || strings.HasPrefix(specPath, "/") {
		candidate := specPath
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(filepath.Dir(importer), filepath.FromSlash(specPath))
		}
		if abs, err := modpath.AbsFile(candidate); err == nil && (iq.Raw || iq.URL || asset.IsExt(filepath.Ext(abs))) {
			if err := r.maybeOnAsset(abs, iq); err != nil {
				return "", false, err
			}
			return abs, false, nil
		}
	}
	if allowModuleField && r.cfg.Format == "esm" {
		if chosen, ok, err := r.preferModuleField(importer, specPath); err != nil || ok {
			return chosen, false, err
		}
	}
	abs, external, err := r.resolveWithCompiler(importer, specPath)
	if err != nil || external {
		return abs, external, err
	}
	if err := r.maybeOnAsset(abs, iq); err != nil {
		return "", false, err
	}
	return abs, false, nil
}

func (r *Resolver) maybeOnAsset(abs string, iq asset.Query) error {
	if r.cfg.OnAsset == nil {
		return nil
	}
	if _, ok := asset.KindForPath(abs, iq); !ok {
		return nil
	}
	return r.cfg.OnAsset(abs, iq)
}

func (r *Resolver) resolveWithCompiler(importer, spec string) (string, bool, error) {
	containing, ok := tspath.TryRootedFilePathFromAbsolute(importer)
	if !ok {
		return "", false, fmt.Errorf("invalid importer path %s", importer)
	}
	resolved, _, err := r.res.ResolveModuleName(spec, containing, r.mode, nil)
	if err != nil {
		return "", false, err
	}
	if resolved == nil || !resolved.IsResolved() {
		if isExternalSpecifier(spec) {
			r.NoteExternal(spec)
			return "", true, nil
		}
		return "", false, fmt.Errorf("cannot resolve %q imported by %s", spec, importer)
	}
	abs, err := modpath.AbsFile(filepath.FromSlash(resolved.ResolvedFileName.AsString()))
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

func (r *Resolver) preferModuleField(importer, spec string) (string, bool, error) {
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
	chosen, external, err := r.resolveSpec(filepath.Join(dir, "package.json"), filepath.ToSlash(rel), false)
	if err != nil || external || chosen == "" {
		return "", false, err
	}
	return chosen, true, nil
}

// NoteExternal records a specifier as external.
func (r *Resolver) NoteExternal(spec string) {
	if r.externals == nil {
		r.externals = map[string]struct{}{}
	}
	r.externals[spec] = struct{}{}
}

// IsUserExternal reports whether spec is listed in Config.External or is external in lib mode.
func (r *Resolver) IsUserExternal(spec string) bool {
	if r.cfg.LibBuild {
		if _, _, ok := packageSpecifier(spec); ok {
			return true
		}
	}
	name, _, ok := packageSpecifier(spec)
	if !ok {
		return false
	}
	for _, ext := range r.cfg.External {
		if spec == ext || name == ext || strings.HasPrefix(spec, ext+"/") {
			return true
		}
	}
	return false
}

// Externals returns specifiers marked external during resolution.
func (r *Resolver) Externals() map[string]struct{} {
	return r.externals
}

func applyAlias(alias map[string]string, spec string) (string, bool) {
	if len(alias) == 0 {
		return "", false
	}
	bestKey := ""
	bestVal := ""
	for key, val := range alias {
		if key == "" {
			continue
		}
		if spec == key || strings.HasPrefix(spec, key+"/") {
			if len(key) > len(bestKey) {
				bestKey, bestVal = key, val
			}
		}
	}
	if bestKey == "" {
		return "", false
	}
	rest := strings.TrimPrefix(spec, bestKey)
	rest = strings.TrimPrefix(rest, "/")
	val := filepath.ToSlash(bestVal)
	if rest == "" {
		return val, true
	}
	if strings.HasSuffix(val, "/") {
		return val + rest, true
	}
	return val + "/" + rest, true
}

// ExportConditions builds CustomConditions for tsgo resolution.
func ExportConditions(extra []string) []string {
	out := []string{"module"}
	seen := map[string]bool{"module": true, "import": true, "require": true, "default": true}
	for _, condition := range extra {
		if condition == "" || seen[condition] {
			continue
		}
		seen[condition] = true
		out = append(out, condition)
	}
	return out
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
	_ = path.Ext(spec)
	return nil
}