package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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
	var literals, globs []string
	for _, pattern := range patterns {
		body := strings.TrimPrefix(pattern, "!")
		if strings.HasPrefix(pattern, "!") || containsGlob(body) {
			globs = append(globs, pattern)
			continue
		}
		literals = append(literals, pattern)
	}
	out := append([]string{}, literals...)
	if len(globs) > 0 {
		matched, err := glob.MatchFiles(globs, glob.Options{
			Dir:             dir,
			Absolute:        true,
			FileOnly:        true,
			GlobStar:        true,
			CaseInsensitive: runtime.GOOS == "windows",
		})
		if err != nil {
			return nil, err
		}
		for _, file := range matched {
			slash := filepath.ToSlash(file)
			if strings.Contains(slash, "/node_modules/") || strings.HasSuffix(strings.ToLower(slash), ".d.ts") {
				continue
			}
			out = append(out, file)
		}
		if len(matched) == 0 {
			return nil, fmt.Errorf("no files matched %s", strings.Join(globs, ", "))
		}
	}
	return uniqueFiles(out)
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
