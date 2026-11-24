package resolve

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/filepath"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/collections"
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
	Alias map[string]string
	// CompilerOptions carries the tsconfig options that change how module
	// specifiers are resolved: "moduleResolution" and "paths".
	CompilerOptions *core.CompilerOptions
	OnAsset         func(abs string, q asset.Query) error
	ResolveHook     func(importer, spec string) (path string, external bool, ok bool, err error)
	// FS reads the files the resolver probes. Nil uses the OS file system.
	FS vfs.FS
	// Mode is the resolution mode tsgo uses for the import graph.
	Mode core.ResolutionMode
	// BaseURL stands in for the removed tsconfig "baseUrl" option. TypeScript
	// 5 resolved bare specifiers against it, so a tsconfig that still uses it
	// keeps working. It is kept apart from the mappings the user wrote so
	// library mode still treats bare package imports as external.
	BaseURL BaseURLFallback
	// CWD resolves relative paths. Empty falls back to the process working
	// directory.
	CWD string
	// EntryDir is the directory of the entry file. Resolution starts here.
	EntryDir string
	Format   string // "esm", "cjs", ...
	// Conditions are the extra package.json "exports" conditions. They are used
	// as given: no platform-specific condition is added.
	Conditions []string
	External   []string
	LibBuild   bool // library mode: bare package imports are external
}

// BaseURLFallback describes the project root that stands in for the removed
// tsconfig "baseUrl" option.
type BaseURLFallback struct {
	// Directory is the project root bare specifiers fall back to.
	Directory tspath.RootedDirectoryPath
	// CaseSensitivity compares path components.
	CaseSensitivity tspath.CaseSensitivity
}

// IsZero reports whether the fallback is unset.
func (f BaseURLFallback) IsZero() bool {
	return f.Directory == ""
}

// Resolver resolves import specifiers using tsgo's bundler resolver.
type Resolver struct {
	fs        *realpathFS
	res       *tsmodule.DefaultResolver
	mode      core.ResolutionMode
	externals map[string]struct{}
	cwd       string
	cfg       Config
}

type resolutionHost struct {
	fs  vfs.FS
	dir tspath.RootedDirectoryPath
}

func (h resolutionHost) FS() vfs.FS { return h.fs }

func (h resolutionHost) GetCurrentDirectory() tspath.RootedDirectoryPath { return h.dir }

// New constructs a Resolver rooted at Config.EntryDir.
func New(cfg Config) (*Resolver, error) {
	dir, err := modpath.AbsPath(cfg.CWD, cfg.EntryDir)
	if err != nil {
		return nil, err
	}
	rooted := tspath.RootedDirectoryPathFromAbsolute(dir)
	base := cfg.FS
	native := base == nil
	if native {
		base = osvfs.FS()
	}
	fs := newRealpathFS(base, native)
	res := tsmodule.NewResolver(tsmodule.ResolverOptions{
		Host:            resolutionHost{fs: fs, dir: rooted},
		CompilerOptions: resolutionCompilerOptions(cfg.Conditions, cfg.CompilerOptions, cfg.BaseURL),
	})
	return &Resolver{
		res:       res,
		fs:        fs,
		cwd:       filepath.ToSlash(cfg.CWD),
		mode:      cfg.Mode,
		cfg:       cfg,
		externals: map[string]struct{}{},
	}, nil
}

// resolutionCompilerOptions builds the compiler options used to resolve module
// specifiers for the JavaScript graph. The tsconfig options that steer
// resolution are honoured; everything else keeps the bundler defaults.
// Config.Conditions is used as given: no platform-specific condition is added
// and the module resolution kind is never changed.
func resolutionCompilerOptions(conditions []string, src *core.CompilerOptions, baseURL BaseURLFallback) *core.CompilerOptions {
	opts := &core.CompilerOptions{
		Module:                     core.ModuleKindESNext,
		ModuleResolution:           core.ModuleResolutionKindBundler,
		AllowJs:                    core.TSTrue,
		AllowImportingTsExtensions: core.TSTrue,
		NoDtsResolution:            core.TSTrue,
		ResolveJsonModule:          core.TSTrue,
		ResolvePackageJsonExports:  core.TSTrue,
		ResolvePackageJsonImports:  core.TSTrue,
		CustomConditions:           ExportConditions(conditions),
	}
	if src != nil {
		opts.Paths = WithPathsFallback(src.Paths, baseURL, src.PathsBasePath)
		opts.PathsBasePath = src.PathsBasePath
		if kind := NormalizeModuleResolution(src.ModuleResolution); kind != core.ModuleResolutionKindUnknown {
			opts.ModuleResolution = kind
		}
	}
	return opts
}

// WithPathsFallback returns the path mappings with the wildcard fallback
// mapping added. The fallback stands in for the removed "baseUrl" option: it
// sends every bare specifier to the project root, the way TypeScript 5 did.
// Mappings the user wrote keep their own priority, so an explicit alias still
// wins over the fallback.
func WithPathsFallback(paths *collections.OrderedMap[string, []string], baseURL BaseURLFallback, basePath tspath.RootedDirectoryPath) *collections.OrderedMap[string, []string] {
	if baseURL.IsZero() {
		return paths
	}
	result := collections.NewOrderedMapWithSizeHint[string, []string](paths.Size() + 1)
	if paths != nil {
		for key, values := range paths.Entries() {
			result.Set(key, values)
		}
	}
	if !result.Has("*") {
		target := baseURL.Directory.ResolveFile("*")
		result.Set("*", []string{RelativeTo(target, basePath, baseURL.CaseSensitivity)})
	}
	return result
}

// RelativeTo returns the path of target relative to basePath. It falls back
// to the target itself when the two cannot be related.
func RelativeTo(target tspath.RootedFilePath, basePath tspath.RootedDirectoryPath, caseSensitivity tspath.CaseSensitivity) string {
	if basePath == "" {
		return target.AsString()
	}
	relative, ok := caseSensitivity.RelativePathFromPath(basePath, target.AsPath())
	if !ok {
		return target.AsString()
	}
	return relative.AsString()
}

// NormalizeModuleResolution maps a tsconfig "moduleResolution" value to the
// kind tsgo understands. The removed "classic" and "node10" kinds fall back to
// "bundler", which is what tsgo does for them as well.
func NormalizeModuleResolution(kind core.ModuleResolutionKind) core.ModuleResolutionKind {
	switch kind {
	case core.ModuleResolutionKindNode16, core.ModuleResolutionKindNodeNext, core.ModuleResolutionKindBundler:
		return kind
	case core.ModuleResolutionKindUnknown:
		return core.ModuleResolutionKindBundler
	default:
		// "classic" and "node10" were removed in TypeScript 7. Their enum
		// values are deprecated, so they are matched by the default branch.
		return core.ModuleResolutionKindBundler
	}
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
			abs, err := modpath.AbsFile(r.fs, r.cwd, joined)
			if err == nil {
				id, err := r.assetResolved(abs, iq)
				if err != nil {
					return "", false, err
				}
				return id, false, nil
			}
		}
	}
	if r.cfg.ResolveHook != nil {
		path, external, ok, hookErr := r.cfg.ResolveHook(importer, specPath)
		if hookErr != nil {
			return "", false, hookErr
		}
		if ok {
			if external {
				name := path
				if name == "" {
					name = specPath
				}
				r.NoteExternal(name)
				return "", true, nil
			}
			abs, err := modpath.AbsFile(r.fs, r.cwd, path)
			if err != nil {
				// A hook may name a file the injected FS does not report, for
				// example a virtual id; keep the cleaned path.
				abs, err = modpath.AbsPath(r.cwd, path)
				if err != nil {
					return "", false, fmt.Errorf("resolve %q from %s: %w", spec, importer, err)
				}
			}
			id, err := r.assetResolved(abs, iq)
			if err != nil {
				return "", false, err
			}
			return id, false, nil
		}
	}
	if err := unsupportedSpecifier(specPath); err != nil {
		return "", false, err
	}
	if isExternalSpecifier(specPath) || r.isListedExternal(specPath) {
		r.NoteExternal(specPath)
		return "", true, nil
	}
	if r.cfg.LibBuild && !r.cfg.BaseURL.IsZero() && !r.matchesPathsAlias(specPath) {
		if _, _, ok := packageSpecifier(specPath); ok {
			// Library mode leaves bare package imports external, but the
			// project root fallback may still point at a local file. Give it
			// a chance before the specifier is left external.
			abs, resolved, err := r.resolveWithCompiler(importer, specPath)
			if err != nil {
				return "", false, err
			}
			if resolved && !isPackageFile(abs) {
				id, err := r.assetResolved(abs, iq)
				if err != nil {
					return "", false, err
				}
				return id, false, nil
			}
			r.NoteExternal(specPath)
			return "", true, nil
		}
	}
	if r.IsUserExternal(specPath) {
		r.NoteExternal(specPath)
		return "", true, nil
	}
	if strings.HasPrefix(specPath, ".") || strings.HasPrefix(specPath, "/") {
		candidate := specPath
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(filepath.Dir(importer), filepath.FromSlash(specPath))
		}
		if abs, err := modpath.AbsFile(r.fs, r.cwd, candidate); err == nil && (iq.Raw || iq.URL || asset.IsExt(modpath.LowerExt(abs))) {
			id, err := r.assetResolved(abs, iq)
			if err != nil {
				return "", false, err
			}
			return id, false, nil
		}
	}
	if allowModuleField && r.cfg.Format == "esm" {
		if chosen, ok, err := r.preferModuleField(importer, specPath); err != nil || ok {
			return chosen, false, err
		}
	}
	abs, resolved, err := r.resolveWithCompiler(importer, specPath)
	if err != nil {
		return "", false, err
	}
	if !resolved {
		return "", false, fmt.Errorf("cannot resolve %q imported by %s", specPath, importer)
	}
	id, err := r.assetResolved(abs, iq)
	if err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (r *Resolver) assetResolved(abs string, iq asset.Query) (string, error) {
	kind, ok := asset.KindForPath(abs, iq)
	if !ok {
		return abs, nil
	}
	if r.cfg.OnAsset != nil {
		if err := r.cfg.OnAsset(abs, iq); err != nil {
			return "", err
		}
	}
	return asset.ModuleKey(abs, kind), nil
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
		return "", false, nil
	}
	// Later reads use this path as the module id, so it must not cross a
	// reparse point the file system refuses to traverse.
	raw := r.fs.realNative(filepath.FromSlash(resolved.ResolvedFileName.AsString()))
	abs, err := modpath.AbsFile(r.fs, r.cwd, raw)
	if err != nil {
		return "", false, fmt.Errorf("cannot resolve %q imported by %s: %w", spec, importer, err)
	}
	if strings.HasSuffix(strings.ToLower(abs), ".d.ts") {
		return "", false, fmt.Errorf("%q imported by %s resolved to declaration file %s", spec, importer, abs)
	}
	if err := unsupportedSpecifier(abs); err != nil {
		return "", false, err
	}
	return abs, true, nil
}

func (r *Resolver) preferModuleField(importer, spec string) (string, bool, error) {
	name, sub, ok := packageSpecifier(spec)
	if !ok || sub != "" {
		return "", false, nil
	}
	dir, meta, ok := findPackageMeta(r.fs, importer, name)
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
	if _, _, ok := packageSpecifier(spec); !ok {
		return false
	}
	if r.isListedExternal(spec) {
		return true
	}
	if r.cfg.LibBuild {
		// A tsconfig "paths" alias points at a local file, so it is not a
		// package import and stays in the bundle.
		if r.matchesPathsAlias(spec) {
			return false
		}
		return true
	}
	return false
}

// isListedExternal reports whether spec is listed in Config.External.
func (r *Resolver) isListedExternal(spec string) bool {
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

// matchesPathsAlias reports whether spec matches one of the tsconfig "paths"
// patterns. Such a specifier resolves to a local file instead of a package.
func (r *Resolver) matchesPathsAlias(spec string) bool {
	if r.cfg.CompilerOptions == nil {
		return false
	}
	paths := r.cfg.CompilerOptions.Paths
	if paths == nil || paths.Size() == 0 {
		return false
	}
	for pattern := range paths.Keys() {
		if matchPathPattern(pattern, spec) {
			return true
		}
	}
	return false
}

// matchPathPattern reports whether spec matches a tsconfig "paths" pattern.
// A pattern holds at most one "*" wildcard.
func matchPathPattern(pattern, spec string) bool {
	before, after, ok := strings.Cut(pattern, "*")
	if !ok {
		return pattern == spec
	}
	prefix, suffix := before, after
	if !strings.HasPrefix(spec, prefix) || !strings.HasSuffix(spec, suffix) {
		return false
	}
	return len(spec) >= len(prefix)+len(suffix)
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

// isPackageFile reports whether path points into a package folder, so it
// belongs to a dependency rather than to the project itself.
func isPackageFile(path string) bool {
	_, ok := modpath.NodeModulesRel(path)
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
	Exports jsontext.Value `json:"exports"`
	Module  string         `json:"module"`
}

func (m packageMeta) hasExports() bool {
	body := bytes.TrimSpace(m.Exports)
	return len(body) > 0 && !bytes.Equal(body, []byte("null")) && !bytes.Equal(body, []byte("false"))
}

func findPackageMeta(fs vfs.FS, importer, name string) (string, packageMeta, bool) {
	dir := filepath.Dir(importer)
	for {
		pkgJSON := filepath.Join(dir, "node_modules", filepath.FromSlash(name), "package.json")
		raw, err := modpath.ReadFile(fs, pkgJSON)
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
	return nil
}
