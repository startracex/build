package wrap

import (
	"fmt"
	"strings"
)

const (
	FormatESM  = "esm"
	FormatCJS  = "cjs"
	FormatIIFE = "iife"
	FormatUMD  = "umd"
)

// WrapFormat converts a CJS-style linked body (module.exports) into IIFE or UMD.
// ESM and CJS pass through unchanged.
func WrapFormat(js string, format string, globalName string) string {
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
	body := ensureCJSBody(strings.TrimSpace(js))
	return fmt.Sprintf(
		"var %s = (function () {\n  var exports = {};\n  var module = { exports: exports };\n%s\n  return module.exports;\n})();\n",
		name,
		indentBlock(body, "  "),
	)
}

func wrapUMD(js string, globalName string) string {
	name := globalName
	if name == "" {
		name = "bundle"
	}
	body := ensureCJSBody(strings.TrimSpace(js))
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

func ensureCJSBody(src string) string {
	if strings.Contains(src, "module.exports") {
		return src
	}
	return stripESMExportsToAssignments(src)
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
