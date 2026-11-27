package resolve

import (
	"slices"
	"testing"

	"github.com/startracex/tsgo/pkg/core"
)

// TestResolutionCompilerOptionsUsesConditionsAsGiven checks the resolver keeps tsconfig moduleResolution and adds no platform condition.
func TestResolutionCompilerOptionsUsesConditionsAsGiven(t *testing.T) {
	src := &core.CompilerOptions{ModuleResolution: core.ModuleResolutionKindNodeNext}

	opts := resolutionCompilerOptions([]string{"browser"}, src, BaseURLFallback{})
	if opts.ModuleResolution != core.ModuleResolutionKindNodeNext {
		t.Fatalf("moduleResolution = %v, want the tsconfig value", opts.ModuleResolution)
	}
	if !slices.Contains(opts.CustomConditions, "module") {
		t.Fatalf("module condition missing: %v", opts.CustomConditions)
	}
	if !slices.Contains(opts.CustomConditions, "browser") {
		t.Fatalf("caller condition missing: %v", opts.CustomConditions)
	}
	if opts.NoDtsResolution != core.TSTrue {
		t.Fatalf("declaration resolution should stay off for the JavaScript graph")
	}
}

// TestResolutionCompilerOptionsNoImplicitBrowser checks no condition is added when the caller supplies none.
func TestResolutionCompilerOptionsNoImplicitBrowser(t *testing.T) {
	opts := resolutionCompilerOptions(nil, nil, BaseURLFallback{})
	if slices.Contains(opts.CustomConditions, "browser") {
		t.Fatalf("browser condition added without being asked: %v", opts.CustomConditions)
	}
	if opts.ModuleResolution != core.ModuleResolutionKindBundler {
		t.Fatalf("default moduleResolution = %v, want bundler", opts.ModuleResolution)
	}
}

// TestResolutionCompilerOptionsDeduplicatesConditions checks repeated conditions collapse to one entry.
func TestResolutionCompilerOptionsDeduplicatesConditions(t *testing.T) {
	opts := resolutionCompilerOptions([]string{"browser", "browser"}, nil, BaseURLFallback{})
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

func TestNewStoresConfig(t *testing.T) {
	dir := t.TempDir()
	res, err := New(Config{
		EntryDir:   dir,
		Conditions: []string{"browser"},
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.cfg.EntryDir != dir {
		t.Fatalf("EntryDir = %q, want %q", res.cfg.EntryDir, dir)
	}
	if len(res.cfg.Conditions) != 1 || res.cfg.Conditions[0] != "browser" {
		t.Fatalf("Conditions = %v", res.cfg.Conditions)
	}
}
