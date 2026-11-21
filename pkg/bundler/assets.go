package bundler

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type assetKind int

const (
	assetNone assetKind = iota
	assetRaw
	assetURL
	assetCSS
)

type assetUnit struct {
	kind     assetKind
	source   string // absolute source path
	data     []byte
	text     string // for raw
	outRel   string // relative emit path
	jsModule string // synthetic ESM for the importer
}

type importQuery struct {
	path  string
	raw   bool
	url   bool
	query string
}

func parseImportQuery(spec string) importQuery {
	base, q, ok := strings.Cut(spec, "?")
	if !ok {
		return importQuery{path: spec}
	}
	iq := importQuery{path: base, query: q}
	for _, part := range strings.Split(q, "&") {
		switch part {
		case "raw":
			iq.raw = true
		case "url":
			iq.url = true
		}
	}
	return iq
}

func isAssetExt(ext string) bool {
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

func (b *bundler) loadAsset(abs string, kind assetKind) error {
	id := moduleID(abs)
	if _, ok := b.units[id]; ok {
		return nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read asset %s: %w", abs, err)
	}
	rel := filepath.Base(abs)
	if b.opts.RootDir != "" {
		if r, err := filepath.Rel(b.opts.RootDir, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	rel = filepath.ToSlash(rel)
	au := &assetUnit{kind: kind, source: abs, data: data, outRel: rel}
	var js string
	switch kind {
	case assetRaw:
		au.text = string(data)
		js = "export default " + strconv.Quote(au.text) + ";\n"
	case assetCSS:
		js = "export {};\n"
	case assetURL:
		url := joinPublicPath(b.opts.PublicPath, rel)
		js = "export default " + strconv.Quote(url) + ";\n"
	default:
		url := joinPublicPath(b.opts.PublicPath, rel)
		js = "export default " + strconv.Quote(url) + ";\n"
		au.kind = assetURL
	}
	au.jsModule = js
	b.assets[id] = au
	b.units[id] = &unit{code: js, source: string(data), asset: true, json: false}
	return nil
}

func joinPublicPath(public, rel string) string {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if public == "" {
		return "./" + strings.TrimPrefix(rel, "./")
	}
	if strings.HasSuffix(public, "/") {
		return public + strings.TrimPrefix(rel, "/")
	}
	return public + "/" + strings.TrimPrefix(rel, "/")
}

func (b *bundler) emitAssetFiles(result *Result) {
	if result.Manifest == nil {
		result.Manifest = map[string]string{}
	}
	outRoot := b.opts.OutDir
	if outRoot == "" && b.opts.OutFile != "" {
		outRoot = filepath.Dir(b.opts.OutFile)
	}
	if outRoot == "" {
		outRoot = "."
	}
	for _, au := range b.assets {
		if au.kind == assetRaw {
			result.Manifest[au.source] = "(inline)"
			continue
		}
		dest := filepath.Join(outRoot, filepath.FromSlash(au.outRel))
		result.Files = append(result.Files, Output{Path: dest, Data: string(au.data)})
		result.Manifest[au.source] = dest
	}
}

func manifestOutput(opts Options, manifest map[string]string) []Output {
	if len(manifest) == 0 {
		manifest = map[string]string{}
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	path := "manifest.json"
	if opts.OutDir != "" {
		path = filepath.Join(opts.OutDir, "manifest.json")
	} else if opts.OutFile != "" {
		path = filepath.Join(filepath.Dir(opts.OutFile), "manifest.json")
	}
	return []Output{{Path: path, Data: string(raw) + "\n"}}
}
