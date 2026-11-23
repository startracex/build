package resolve

import (
	"slices"
	"testing"

	"github.com/startracex/tsgo/pkg/core"
)

func TestResolutionCompilerOptionsPlatform(t *testing.T) {
	src := &core.CompilerOptions{ModuleResolution: core.ModuleResolutionKindNodeNext}

	node := resolutionCompilerOptions(nil, PlatformNode, src, BaseURLFallback{})
	if node.ModuleResolution != core.ModuleResolutionKindNodeNext {
		t.Fatalf("node platform moduleResolution = %v", node.ModuleResolution)
	}
	if slices.Contains(node.CustomConditions, "browser") {
		t.Fatalf("node platform should not add the browser condition: %v", node.CustomConditions)
	}

	neutral := resolutionCompilerOptions(nil, PlatformNeutral, src, BaseURLFallback{})
	if neutral.ModuleResolution != core.ModuleResolutionKindNodeNext {
		t.Fatalf("neutral platform moduleResolution = %v", neutral.ModuleResolution)
	}

	browser := resolutionCompilerOptions(nil, PlatformBrowser, src, BaseURLFallback{})
	if browser.ModuleResolution != core.ModuleResolutionKindBundler {
		t.Fatalf("browser platform should use bundler resolution, got %v", browser.ModuleResolution)
	}
	if !slices.Contains(browser.CustomConditions, "browser") {
		t.Fatalf("browser platform should add the browser condition: %v", browser.CustomConditions)
	}
	if !slices.Contains(browser.CustomConditions, "module") {
		t.Fatalf("browser platform dropped the module condition: %v", browser.CustomConditions)
	}

	// A platform switch only touches resolution kind and conditions.
	if browser.NoDtsResolution != core.TSTrue {
		t.Fatalf("declaration resolution should stay off for the JavaScript graph")
	}
}

func TestResolutionCompilerOptionsDuplicatesBrowserCondition(t *testing.T) {
	opts := resolutionCompilerOptions([]string{"browser"}, PlatformBrowser, nil, BaseURLFallback{})
	count := 0
	for _, condition := range opts.CustomConditions {
		if condition == "browser" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("browser condition repeated %d times: %v", count, opts.CustomConditions)
	}
}

func TestParsePlatform(t *testing.T) {
	cases := map[string]Platform{
		"":         PlatformAuto,
		"browser":  PlatformBrowser,
		"node":     PlatformNode,
		"neutral":  PlatformNeutral,
		" browser": PlatformBrowser,
	}
	for name, want := range cases {
		got, err := ParsePlatform(name)
		if err != nil {
			t.Fatalf("ParsePlatform(%q): %v", name, err)
		}
		if got != want {
			t.Fatalf("ParsePlatform(%q) = %q, want %q", name, got, want)
		}
	}
	if _, err := ParsePlatform("deno"); err == nil {
		t.Fatalf("unknown platform should be rejected")
	}
}
