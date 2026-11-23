package define

import (
	"regexp"
	"strings"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

var (
	reDebugger    = regexp.MustCompile(`(?m)^\s*debugger\s*;?\s*$`)
	reConsoleCall = regexp.MustCompile(`(?m)^\s*console\.[a-zA-Z_]\w*\s*\([^;]*\)\s*;?\s*$`)
)

// ApplyDefineDrop applies define replacements and drop directives to transpiled source.
func ApplyDefineDrop(src string, define map[string]string, drop []string) string {
	if len(drop) == 0 && len(define) == 0 {
		return foldConstantBranches(src)
	}
	out := src
	dropConsole, dropDebugger := false, false
	for _, d := range drop {
		switch strings.ToLower(strings.TrimSpace(d)) {
		case "console":
			dropConsole = true
		case "debugger":
			dropDebugger = true
		}
	}
	if dropDebugger {
		out = reDebugger.ReplaceAllString(out, "")
	}
	if dropConsole {
		out = stripConsoleCalls(out)
	}
	if len(define) > 0 {
		out = applyDefines(out, define)
	}
	return foldConstantBranches(out)
}

func stripConsoleCalls(src string) string {
	file := jsedit.ParseEmittedJS(src)
	if file.Statements == nil {
		return reConsoleCall.ReplaceAllString(src, "")
	}
	var edits []jsedit.Edit
	for _, stmt := range file.Statements.Nodes {
		if stmt.Kind != ast.KindExpressionStatement {
			continue
		}
		expr := stmt.AsExpressionStatement().Expression
		if isConsoleCall(expr) {
			edits = append(edits, jsedit.Edit{Start: stmt.Pos(), End: stmt.End(), Text: ""})
		}
	}
	if len(edits) == 0 {
		return reConsoleCall.ReplaceAllString(src, "")
	}
	out, err := jsedit.ApplyEdits(src, edits)
	if err != nil {
		return reConsoleCall.ReplaceAllString(src, "")
	}
	return out
}

func isConsoleCall(n *ast.Node) bool {
	if n == nil || n.Kind != ast.KindCallExpression {
		return false
	}
	callee := n.AsCallExpression().Expression
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := callee.AsPropertyAccessExpression()
	return access.Expression != nil &&
		access.Expression.Kind == ast.KindIdentifier &&
		access.Expression.Text() == "console"
}

func applyDefines(src string, define map[string]string) string {
	keys := make([]string, 0, len(define))
	for k := range define {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) > len(keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	out := src
	for _, key := range keys {
		val := define[key]
		if strings.HasPrefix(key, "process.env.") {
			out = strings.ReplaceAll(out, key, val)
			continue
		}
		re, err := regexp.Compile(`\b` + regexp.QuoteMeta(key) + `\b`)
		if err != nil {
			continue
		}
		out = re.ReplaceAllString(out, val)
	}
	return out
}
