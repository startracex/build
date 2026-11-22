package bundler

import "github.com/startracex/build/pkg/modpath"

// Local helpers for the few call sites that still use short names.
// Prefer importing pkg/modpath, pkg/jsedit, pkg/sourcemap, pkg/resolve directly.

func moduleID(path string) string { return modpath.ModuleID(path) }

func javascriptOutputAbs(abs string) string { return modpath.JavascriptOutputAbs(abs) }
