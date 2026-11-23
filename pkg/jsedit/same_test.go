package jsedit

import (
	"testing"

	"github.com/startracex/tsgo/pkg/ast"
)

func TestSameTokensIgnoresFormatting(t *testing.T) {
	original := `export function mega(a,b){return a+b}export const tiny={x:1,y:2};`
	reprinted := "export function mega(a, b) { return a + b; }\n\nexport const tiny = { x: 1, y: 2 };\n"
	if !SameTokens(original, ParseEmittedJS(original), reprinted, ParseEmittedJS(reprinted)) {
		t.Fatal("a reprint of the same program should compare equal")
	}
}

func TestSameTokensDetectsTransforms(t *testing.T) {
	cases := []struct{ name, from, to string }{
		{"operator", "const a = b ?? c;", "const a = b !== null && b !== void 0 ? b : c;"},
		{"optional", "const a = b?.c;", `const a = b === null || b === void 0 ? void 0 : b.c;`},
		{"class field", "class A { x = 1; }", "class A { constructor() { this.x = 1; } }"},
		{"added statement", "const a = 1;", "const a = 1;\nexport {};"},
		{"literal", "const a = 1;", "const a = 2;"},
		{"binding", "const a = 1;", "const b = 1;"},
		{"removed statement", "const a = 1;\nconst b = 2;", "const a = 1;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if SameTokens(tc.from, ParseEmittedJS(tc.from), tc.to, ParseEmittedJS(tc.to)) {
				t.Fatalf("%q and %q should differ", tc.from, tc.to)
			}
		})
	}
}

func TestSameTokensReportsParseErrors(t *testing.T) {
	bad := "const a = ;"
	if !SameTokens(bad, ParseEmittedJS(bad), bad, ParseEmittedJS(bad)) {
		t.Fatal("equal text must compare equal even when it does not parse")
	}
	if SameTokens("const a = 1;", ParseEmittedJS("const a = 1;"), "", (*ast.SourceFile)(nil)) {
		t.Fatal("a missing program must not compare equal")
	}
}
