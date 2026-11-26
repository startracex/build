package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/startracex/build/pkg/asset"
	"github.com/startracex/build/pkg/modpath"
	"github.com/startracex/wangpkg/pkg/glob"
)

func expandEntries(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	caseInsensitive := runtime.GOOS == "windows"
	var literals, globs, excludes []string
	for _, pattern := range patterns {
		body, excluded := strings.CutPrefix(pattern, "!")
		if excluded {
			globs = append(globs, pattern)
			excludes = append(excludes, body)
			continue
		}
		if containsGlob(body) {
			globs = append(globs, pattern)
			continue
		}
		literals = append(literals, pattern)
	}

	excludeRes := make([]*regexp.Regexp, 0, len(excludes))
	for _, pattern := range excludes {
		pattern = filepath.ToSlash(filepath.Clean(pattern))
		excludeRes = append(excludeRes, glob.GlobToRegExp(pattern, true, caseInsensitive))
	}

	var out []string
	for _, literal := range literals {
		files, err := expandDirectory(literal, dir, excludeRes)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	if len(globs) > 0 {
		matched, err := glob.MatchFiles(globs, glob.Options{
			Dir:             dir,
			Absolute:        true,
			FileOnly:        true,
			GlobStar:        true,
			CaseInsensitive: caseInsensitive,
		})
		if err != nil {
			return nil, err
		}
		for _, file := range matched {
			if entryFile(file) {
				out = append(out, file)
			}
		}
		if len(matched) == 0 && len(excludes) < len(globs) {
			return nil, fmt.Errorf("no files matched %s", strings.Join(globs, ", "))
		}
	}
	return uniqueFiles(out)
}

// expandDirectory turns a directory into entry files; other paths pass through so the bundler reports them.
func expandDirectory(path, cwd string, excludes []*regexp.Regexp) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return []string{path}, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(absPath, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if current == absPath {
				return nil
			}
			name := d.Name()
			if strings.EqualFold(name, "node_modules") || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !entryFile(current) {
			return nil
		}
		rel, err := filepath.Rel(cwd, current)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		for _, re := range excludes {
			if re.MatchString(rel) {
				return nil
			}
		}
		files = append(files, current)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no source files found in %s", path)
	}
	return files, nil
}

func entryFile(path string) bool {
	if modpath.IsDeclaration(path) {
		return false
	}
	if _, inDeps := modpath.NodeModulesRel(path); inDeps {
		return false
	}
	return asset.IsSource(path) || modpath.LowerExt(path) == ".html"
}

func uniqueFiles(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		key := filepath.ToSlash(abs)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	sort.Strings(out)
	return out, nil
}
