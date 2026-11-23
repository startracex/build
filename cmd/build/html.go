package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/startracex/build/pkg/bundler"
	"github.com/startracex/build/pkg/modpath"
	"golang.org/x/net/html"
)

type htmlDocument struct {
	path    string
	node    *html.Node
	scripts map[*html.Node]string
	styles  map[*html.Node]string
}

func prepareHTMLEntries(entries []string) ([]*htmlDocument, []string, error) {
	var docs []*htmlDocument
	var modules []string
	for _, entry := range entries {
		if !modpath.IsHTMLPath(entry) {
			modules = append(modules, entry)
			continue
		}
		doc, err := parseHTMLDocument(entry)
		if err != nil {
			return nil, nil, err
		}
		docs = append(docs, doc)
		for _, src := range doc.scripts {
			modules = append(modules, src)
		}
	}
	if len(docs) == 0 {
		return nil, entries, nil
	}
	cleaned, err := uniqueFiles(modules)
	if err != nil {
		return nil, nil, err
	}
	return docs, cleaned, nil
}

func parseHTMLDocument(path string) (*htmlDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	root, err := html.Parse(file)
	if err != nil {
		return nil, err
	}
	doc := &htmlDocument{
		path:    path,
		node:    root,
		scripts: map[*html.Node]string{},
		styles:  map[*html.Node]string{},
	}
	dir := filepath.Dir(path)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script":
				src := htmlAttr(node, "src")
				if src != "" && !externalReference(src) {
					if abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(src))); err == nil && entryFile(abs) {
						doc.scripts[node] = abs
					}
				}
			case "link":
				rel := strings.ToLower(htmlAttr(node, "rel"))
				href := htmlAttr(node, "href")
				if strings.Contains(rel, "stylesheet") && href != "" && !externalReference(href) {
					if abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(href))); err == nil {
						doc.styles[node] = abs
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return doc, nil
}

func renderHTMLDocuments(docs []*htmlDocument, opts bundler.Options, result *bundler.Result) ([]bundler.Output, error) {
	outRoot := opts.OutDir
	if outRoot == "" && opts.OutFile != "" {
		outRoot = filepath.Dir(opts.OutFile)
	}
	if outRoot == "" {
		outRoot = "."
	}
	root := opts.RootDir
	if root == "" && len(docs) > 0 {
		root = filepath.Dir(docs[0].path)
	}
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = cwd
	}

	var extra []bundler.Output
	copiedCSS := map[string]bool{}
	for _, doc := range docs {
		htmlPath := outputPathUnder(root, outRoot, doc.path)
		htmlDir := filepath.Dir(htmlPath)
		for node, src := range doc.scripts {
			if out := result.Sources[filepath.ToSlash(src)]; out != "" {
				setHTMLAttr(node, "src", relativeURL(htmlDir, out))
			}
		}
		for node, src := range doc.styles {
			cssPath := outputPathUnder(root, outRoot, src)
			if !copiedCSS[cssPath] {
				data, err := os.ReadFile(src)
				if err != nil {
					return nil, err
				}
				extra = append(extra, bundler.Output{Path: cssPath, Data: string(data)})
				copiedCSS[cssPath] = true
			}
			setHTMLAttr(node, "href", relativeURL(htmlDir, cssPath))
		}
		var buf bytes.Buffer
		if err := html.Render(&buf, doc.node); err != nil {
			return nil, err
		}
		extra = append(extra, bundler.Output{Path: htmlPath, Data: buf.String()})
	}
	return extra, nil
}

func outputPathUnder(root, outRoot, source string) string {
	rel, err := filepath.Rel(root, source)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(source)
	}
	return filepath.Join(outRoot, rel)
}

func relativeURL(fromDir, to string) string {
	rel, err := filepath.Rel(fromDir, to)
	if err != nil {
		return filepath.ToSlash(to)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func externalReference(ref string) bool {
	return strings.HasPrefix(ref, "//") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:")
}

func htmlAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func setHTMLAttr(node *html.Node, key, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == key {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}
