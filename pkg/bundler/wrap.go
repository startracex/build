package bundler

import (
	"fmt"
	"strings"
)

// wrapFormat converts linked ESM/CJS body into IIFE or UMD when requested.
// ESM and CJS pass through; linkESM already emits the right shape for those.
func wrapFormat(js string, format Format, globalName string) string {
	switch format {
	case FormatIIFE:
		return wrapIIFE(js, globalName)
	case FormatUMD:
		return wrapUMD(js, globalName)
	default:
		return js
	}
}

func wrapIIFE(js string, globalName string) string {
	name := globalName
	if name == "" {
		name = "exports"
	}
	body := strings.TrimSpace(js)
	// linkESM CJS path uses module.exports; for IIFE we force CJS-style link
	// then assign onto the global. Prefer rewriting from ESM export form.
	if strings.Contains(body, "module.exports") {
		return fmt.Sprintf("var %s = (function () {\n%s\n  return typeof module !== \"undefined\" && module.exports ? module.exports : {};\n})();\n", name, indentBlock(body, "  "))
	}
	// ESM-looking: wrap and expose via return of bindings is hard; emit as
	// side-effect IIFE that attaches named exports onto the global object.
	return fmt.Sprintf("(function (global) {\n  var exports = {};\n%s\n  global[%q] = exports;\n})(typeof globalThis !== \"undefined\" ? globalThis : typeof window !== \"undefined\" ? window : this);\n",
		indentBlock(stripESMExportsToAssignments(body), "  "), name)
}

func wrapUMD(js string, globalName string) string {
	name := globalName
	if name == "" {
		name = "bundle"
	}
	body := strings.TrimSpace(js)
	if !strings.Contains(body, "module.exports") {
		body = stripESMExportsToAssignments(body)
	}
	return fmt.Sprintf(`(function (root, factory) {
  if (typeof define === "function" && define.amd) {
    define([], factory);
  } else if (typeof module === "object" && module.exports) {
    module.exports = factory();
  } else {
    root[%q] = factory();
  }
})(typeof self !== "undefined" ? self : this, function () {
  var exports = {};
  var module = { exports: exports };
%s
  return module.exports;
});
`, name, indentBlock(body, "  "))
}

func indentBlock(src, pad string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// stripESMExportsToAssignments is a best-effort rewrite for wrapping ESM link
// output when the caller requested IIFE/UMD without going through FormatCJS.
func stripESMExportsToAssignments(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	inExport := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "export {") {
			inExport = true
			continue
		}
		if inExport {
			if strings.Contains(trim, "}") {
				inExport = false
			}
			continue
		}
		if strings.HasPrefix(trim, "export default ") {
			rest := strings.TrimPrefix(trim, "export default ")
			out = append(out, "exports.default = "+rest)
			continue
		}
		if strings.HasPrefix(trim, "export ") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
