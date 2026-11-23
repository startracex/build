package asset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/tsgo/pkg/vfs"
)

// Kind classifies how an import is treated at emit time.
type Kind int

const (
	None Kind = iota
	Raw
	URL
	CSS
)

// Unit is one loaded asset module.
type Unit struct {
	Kind     Kind
	Source   string // absolute source path
	Data     []byte
	Text     string // for raw
	OutRel   string // relative emit path
	JSModule string // synthetic ESM for the importer
}

// Query holds ?raw / ?url flags from an import specifier.
type Query struct {
	Path  string
	Raw   bool
	URL   bool
	Query string
}

// ParseQuery splits a specifier into path and import-query flags.
func ParseQuery(spec string) Query {
	base, q, ok := strings.Cut(spec, "?")
	if !ok {
		return Query{Path: spec}
	}
	iq := Query{Path: base, Query: q}
	for part := range strings.SplitSeq(q, "&") {
		switch part {
		case "raw":
			iq.Raw = true
		case "url":
			iq.URL = true
		}
	}
	return iq
}

// ModuleKey is the graph id for an asset, including ?raw / ?url when needed so
// the same file can be imported under different query kinds.
func ModuleKey(abs string, kind Kind) string {
	id := filepath.ToSlash(abs)
	switch kind {
	case Raw:
		return id + "?raw"
	case URL:
		return id + "?url"
	default:
		return id
	}
}

// KindForPath picks an asset kind from query flags and file extension.
func KindForPath(abs string, q Query) (Kind, bool) {
	ext := modpath.LowerExt(abs)
	switch {
	case q.Raw:
		return Raw, true
	case q.URL:
		return URL, true
	case ext == ".css", ext == ".scss", ext == ".sass", ext == ".less":
		return CSS, true
	case IsExt(ext):
		return URL, true
	default:
		return None, false
	}
}

// IsExt reports whether ext (with dot) is a known static asset extension.
func IsExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".css", ".scss", ".sass", ".less",
		".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif",
		".woff", ".woff2", ".ttf", ".eot",
		".txt", ".md", ".svgz":
		return true
	default:
		return false
	}
}

// JoinPublicPath joins public URL prefix with a relative asset path.
func JoinPublicPath(public, rel string) string {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if public == "" {
		return "./" + strings.TrimPrefix(rel, "./")
	}
	if strings.HasSuffix(public, "/") {
		return public + strings.TrimPrefix(rel, "/")
	}
	return public + "/" + strings.TrimPrefix(rel, "/")
}

// Load reads an asset file through fs and returns the unit plus synthetic ESM
// source. A nil fs reads the OS file system.
func Load(fs vfs.FS, abs string, kind Kind, rootDir, publicPath string) (*Unit, string, error) {
	data, err := modpath.ReadFile(fs, abs)
	if err != nil {
		return nil, "", fmt.Errorf("read asset %s: %w", abs, err)
	}
	rel := filepath.Base(abs)
	if rootDir != "" {
		if r, err := filepath.Rel(rootDir, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	rel = filepath.ToSlash(rel)
	if kind == URL || (kind != Raw && kind != CSS) {
		rel = hashedRel(rel, data)
	}
	au := &Unit{Kind: kind, Source: abs, Data: data, OutRel: rel}
	var js string
	switch kind {
	case Raw:
		au.Text = string(data)
		js = "export default " + strconv.Quote(au.Text) + ";\n"
	case CSS:
		js = "export {};\n"
	case URL:
		url := JoinPublicPath(publicPath, rel)
		js = "export default " + strconv.Quote(url) + ";\n"
	default:
		url := JoinPublicPath(publicPath, rel)
		js = "export default " + strconv.Quote(url) + ";\n"
		au.Kind = URL
	}
	au.JSModule = js
	return au, js, nil
}

// EmitFile is one emitted asset file on disk.
type EmitFile struct {
	Path string
	Data string
}

// CollectEmit builds output files and a manifest from loaded assets.
func CollectEmit(assets map[string]*Unit, outRoot string) (files []EmitFile, manifest map[string]string) {
	manifest = map[string]string{}
	if outRoot == "" {
		outRoot = "."
	}
	for _, au := range assets {
		if au.Kind == Raw {
			manifest[au.Source] = "(inline)"
			continue
		}
		dest := filepath.Join(outRoot, filepath.FromSlash(au.OutRel))
		files = append(files, EmitFile{Path: dest, Data: string(au.Data)})
		manifest[au.Source] = dest
	}
	return files, manifest
}

// ManifestPath returns the path for manifest.json beside bundle output.
func ManifestPath(outDir, outFile string) string {
	if outDir != "" {
		return filepath.Join(outDir, "manifest.json")
	}
	if outFile != "" {
		return filepath.Join(filepath.Dir(outFile), "manifest.json")
	}
	return "manifest.json"
}

func hashedRel(rel string, data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])[:8]
	ext := modpath.Ext(rel)
	stem := modpath.Stem(rel)
	if stem == "" {
		stem = "asset"
	}
	return stem + "-" + hash + ext
}

// SourceExtensions lists the extensions the build accepts as input.
var SourceExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".json"}

// IsSource reports whether path names a file the build accepts as input.
func IsSource(path string) bool {
	return modpath.HasAnyExt(path, SourceExtensions...)
}

// SupportedSource reports whether path can be loaded as source or asset.
func SupportedSource(path string) bool {
	if IsSource(path) {
		return true
	}
	return IsExt(modpath.LowerExt(path))
}
