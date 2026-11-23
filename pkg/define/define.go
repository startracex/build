// Package define rewrites transpiled JavaScript for the define and drop build
// options.
//
// Every rewrite is an edit over one parse of the module, so a define key is
// only replaced where it is a free reference: obj.__DEV__ and "__DEV__" are
// left alone, and a define value must be a single JavaScript expression.
// Removals replace a whole AST node; a removal that sits in an expression slot
// becomes void 0 so the surrounding expression stays evaluable and the call's
// side effects are gone.
//
// The edits of every pass are returned as stages so the caller can keep a
// source map aligned with the rewritten code.
package define

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/startracex/build/pkg/jsedit"
	"github.com/startracex/tsgo/pkg/ast"
)

// maxFoldPasses bounds the constant-folding loop. Folding is applied one level
// per pass, so this only limits how deep a constant branch may nest.
const maxFoldPasses = 16

// voidExpr is what a removed expression becomes. The value is undefined, like
// the removed call's return value, and the operand is side-effect free.
const voidExpr = "void 0"

// Stage is one edit pass over a module. Source is the text the edits apply to;
// a caller that owns a source map remaps it with each stage in order so
// generated positions stay aligned with the rewritten code.
type Stage struct {
	Source string
	Edits  []jsedit.Edit
}

// rule is one compiled define directive.
type rule struct {
	value string
	wrap  bool
}

// ApplyDefineDrop rewrites src with define replacements, drop directives, and
// constant branch folding. It returns the new source and the stages that
// produced it.
func ApplyDefineDrop(src string, defines map[string]string, drop []string) (string, []Stage, error) {
	rules, err := compile(defines)
	if err != nil {
		return src, nil, err
	}
	dropConsole, dropDebugger, err := compileDrop(drop)
	if err != nil {
		return src, nil, err
	}
	var stages []Stage
	cur := src
	if len(rules) > 0 || dropConsole || dropDebugger {
		if edits := rewrite(cur, rules, dropConsole, dropDebugger); len(edits) > 0 {
			next, err := applyEdits(cur, edits)
			if err != nil {
				return src, nil, err
			}
			stages = append(stages, Stage{Source: cur, Edits: edits})
			cur = next
		}
	}
	for range maxFoldPasses {
		edits := fold(cur)
		if len(edits) == 0 {
			break
		}
		next, err := applyEdits(cur, edits)
		if err != nil {
			return src, nil, err
		}
		stages = append(stages, Stage{Source: cur, Edits: edits})
		cur = next
	}
	return cur, stages, nil
}

// Validate reports the first define key or value, or drop directive, that
// would not rewrite anything meaningful. A malformed value must fail before it
// reaches the output instead of being pasted into the module.
func Validate(defines map[string]string, drop []string) error {
	if _, err := compile(defines); err != nil {
		return err
	}
	_, _, err := compileDrop(drop)
	return err
}

func applyEdits(src string, edits []jsedit.Edit) (string, error) {
	out, err := jsedit.ApplyEdits(src, edits)
	if err != nil {
		return "", fmt.Errorf("define: %w", err)
	}
	return out, nil
}

func compile(defines map[string]string) (map[string]rule, error) {
	if len(defines) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(defines))
	for key := range defines {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rules := make(map[string]rule, len(defines))
	for _, key := range keys {
		if err := validateKey(key); err != nil {
			return nil, err
		}
		value, wrap, err := compileValue(key, defines[key])
		if err != nil {
			return nil, err
		}
		rules[key] = rule{value: value, wrap: wrap}
	}
	return rules, nil
}

func compileDrop(drop []string) (console, debugger bool, err error) {
	for _, directive := range drop {
		switch strings.ToLower(strings.TrimSpace(directive)) {
		case "":
			continue
		case "console":
			console = true
		case "debugger":
			debugger = true
		default:
			return false, false, fmt.Errorf("drop %q: supported values are \"console\" and \"debugger\"", directive)
		}
	}
	return console, debugger, nil
}

// validateKey accepts a free identifier or a dotted property chain, which is
// what the AST matcher can recognize. A key that is not a chain would silently
// match nothing.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("define: empty key")
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("define %q: key has surrounding whitespace", key)
	}
	parts := strings.Split(key, ".")
	for _, part := range parts {
		if !isIdentifierSegment(part) {
			return fmt.Errorf("define %q: %q is not an identifier", key, part)
		}
	}
	if len(parts) != 1 {
		return nil
	}
	switch parts[0] {
	case "this", "import", "super":
		return nil
	}
	if !jsedit.IsIdentifierName(parts[0]) {
		return fmt.Errorf("define %q: %q is a reserved word", key, parts[0])
	}
	return nil
}

func isIdentifierSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for i, r := range segment {
		switch {
		case r == '$' || r == '_' || unicode.IsLetter(r):
		case i > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}

// valueProbe wraps a define value in a declaration so the parser validates it
// with the same grammar the output will use.
const valueProbe = "const __define_value = "

// compileValue validates the replacement text and reports whether it needs
// parentheses when it replaces a reference. Values that are already a primary
// expression bind tightly enough in every position.
func compileValue(key, value string) (string, bool, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", false, fmt.Errorf("define %q: value is empty", key)
	}
	probe := valueProbe + text + ";"
	file := jsedit.ParseEmittedJS(probe)
	for _, diag := range file.Diagnostics() {
		return "", false, fmt.Errorf("define %q: %q is not a JavaScript expression: %s", key, text, diag.MessageText())
	}
	statements := file.Statements
	if statements == nil || len(statements.Nodes) != 1 || statements.Nodes[0].End() != len(probe) {
		return "", false, fmt.Errorf("define %q: %q is not a single JavaScript expression", key, text)
	}
	init := declarationInitializer(statements.Nodes[0])
	if init == nil {
		return "", false, fmt.Errorf("define %q: %q is not a single JavaScript expression", key, text)
	}
	return text, needsWrap(init), nil
}

func declarationInitializer(stmt *ast.Node) *ast.Node {
	if stmt.Kind != ast.KindVariableStatement {
		return nil
	}
	list := stmt.AsVariableStatement().DeclarationList
	if list == nil || list.Kind != ast.KindVariableDeclarationList {
		return nil
	}
	declarations := list.AsVariableDeclarationList().Declarations
	if declarations == nil || len(declarations.Nodes) != 1 {
		return nil
	}
	decl := declarations.Nodes[0]
	if decl.Kind != ast.KindVariableDeclaration {
		return nil
	}
	return decl.AsVariableDeclaration().Initializer
}

// needsWrap reports whether the value must be parenthesized to keep its
// meaning when it replaces a reference inside a larger expression.
func needsWrap(expr *ast.Node) bool {
	switch expr.Kind {
	case ast.KindIdentifier,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindArrayLiteralExpression, ast.KindParenthesizedExpression,
		ast.KindCallExpression, ast.KindNewExpression,
		ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return false
	default:
		// Object literals, ternaries, logical and binary expressions, function
		// and arrow expressions, and await/yield all need parentheses to stay a
		// single operand.
		return true
	}
}
