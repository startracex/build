package define

import (
	"strings"
	"testing"
)

func TestFoldIfFalseTrue(t *testing.T) {
	src := `
if (false) { dead(); }
if (true) { live(); }
if (false) { dead(); } else { kept(); }
if (true) { first(); } else { second(); }
false && removed();
true || alsoRemoved();
keep();
`
	out, stages, err := ApplyDefineDrop(src, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) == 0 {
		t.Fatal("fold reported no edit stages")
	}
	if strings.Contains(out, "dead") || strings.Contains(out, "removed") || strings.Contains(out, "alsoRemoved") || strings.Contains(out, "second") {
		t.Fatalf("fold left dead branches:\n%s", out)
	}
	for _, want := range []string{"live()", "kept()", "first()", "keep()"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n\n") {
		t.Fatalf("fold left a blank line:\n%s", out)
	}
}

func TestDefineFreeIdentifiersOnly(t *testing.T) {
	src := `const local = 1;
const value = __DEV__;
const shadow = { __DEV__: 1, other: __DEV__, ["__DEV__"]: 2 };
label: obj.__DEV__;
`
	out := apply(t, src, map[string]string{"__DEV__": "false"}, nil)
	if !strings.Contains(out, "const value = false;") {
		t.Fatalf("free identifier was not replaced:\n%s", out)
	}
	for _, want := range []string{"{ __DEV__: 1", "label: obj.__DEV__", `["__DEV__"]: 2`} {
		if !strings.Contains(out, want) {
			t.Fatalf("replaced a non-reference %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "false") != 2 {
		t.Fatalf("unexpected replacement count:\n%s", out)
	}
}

func TestDefineSkipsShadowedBinding(t *testing.T) {
	src := `const __DEV__ = 1;
export const flag = __DEV__;
function read(__DEV__) { return __DEV__; }
`
	out := apply(t, src, map[string]string{"__DEV__": "false"}, nil)
	if strings.Contains(out, "false") {
		t.Fatalf("shadowed binding was replaced:\n%s", out)
	}
}

func TestDefinePropertyChain(t *testing.T) {
	src := `export const mode = process.env.NODE_ENV;
export const keep = process.env.HOME;
`
	out := apply(t, src, map[string]string{"process.env.NODE_ENV": `"production"`}, nil)
	if !strings.Contains(out, `const mode = "production";`) {
		t.Fatalf("chain was not replaced:\n%s", out)
	}
	if !strings.Contains(out, "process.env.HOME") {
		t.Fatalf("unrelated chain was replaced:\n%s", out)
	}
}

func TestDefineLongestChainWins(t *testing.T) {
	src := `export const a = process.env.FEATURE;
export const b = process.env.OTHER;
`
	out := apply(t, src, map[string]string{
		"process.env":         "env",
		"process.env.FEATURE": "true",
	}, nil)
	if !strings.Contains(out, "const a = true;") {
		t.Fatalf("longest key did not win:\n%s", out)
	}
	if !strings.Contains(out, "const b = env.OTHER;") {
		t.Fatalf("short key was not used for the rest:\n%s", out)
	}
}

func TestDefineWrapsComplexValue(t *testing.T) {
	src := `export const flag = __DEV__ ? 1 : 2;
export const name = PREFIX.length;
`
	out := apply(t, src, map[string]string{
		"__DEV__": "a && b",
		"PREFIX":  `"prod-" + "x"`,
	}, nil)
	if !strings.Contains(out, "const flag = (a && b) ? 1 : 2;") {
		t.Fatalf("complex value was not parenthesized:\n%s", out)
	}
	if !strings.Contains(out, `const name = ("prod-" + "x").length;`) {
		t.Fatalf("member access on a replaced value broke:\n%s", out)
	}
}

func TestDefineShorthandAndAssignment(t *testing.T) {
	src := `const bag = { __DEV__ };
__DEV__ = 1;
export { bag };
`
	out := apply(t, src, map[string]string{"__DEV__": "false"}, nil)
	if !strings.Contains(out, "const bag = { __DEV__: false };") {
		t.Fatalf("shorthand was not expanded:\n%s", out)
	}
	if !strings.Contains(out, "__DEV__ = 1;") {
		t.Fatalf("assignment target was replaced:\n%s", out)
	}
}

func TestDefineConstantBranchFolds(t *testing.T) {
	src := `if (__DEV__) {
  devOnly();
}
while (true) {
  keep();
}
`
	out := apply(t, src, map[string]string{"__DEV__": "false"}, nil)
	if strings.Contains(out, "devOnly") {
		t.Fatalf("dead branch was kept:\n%s", out)
	}
	if !strings.Contains(out, "keep()") {
		t.Fatalf("live code was dropped:\n%s", out)
	}
}

func TestDefineDropsConsoleStatements(t *testing.T) {
	src := `console.log("drop-me");
const label = console.log("in-expression");
if (console.debug("cond")) {
  keep();
}
`
	out := apply(t, src, nil, []string{"console"})
	if strings.Contains(out, "drop-me") {
		t.Fatalf("statement call was kept:\n%s", out)
	}
	if !strings.Contains(out, "const label = void 0;") {
		t.Fatalf("expression call was not replaced by void 0:\n%s", out)
	}
	if !strings.Contains(out, "if (void 0)") {
		t.Fatalf("condition call was not replaced by void 0:\n%s", out)
	}
	if !strings.Contains(out, "keep()") {
		t.Fatalf("unrelated code was dropped:\n%s", out)
	}
}

func TestDefineDropKeepsShadowedConsole(t *testing.T) {
	src := `function quiet(console) {
  console.log("kept");
}
`
	out := apply(t, src, nil, []string{"console"})
	if !strings.Contains(out, `console.log("kept")`) {
		t.Fatalf("shadowed console was dropped:\n%s", out)
	}
}

func TestDefineDropDebuggerInRequiredSlot(t *testing.T) {
	src := `debugger;
if (ready) debugger;
while (ready) {
  debugger;
  keep();
}
`
	out := apply(t, src, nil, []string{"debugger"})
	if strings.Contains(out, "debugger") {
		t.Fatalf("debugger was kept:\n%s", out)
	}
	if !strings.Contains(out, "if (ready) {}") {
		t.Fatalf("required statement slot was not kept valid:\n%s", out)
	}
	if !strings.Contains(out, "keep()") {
		t.Fatalf("code around debugger was dropped:\n%s", out)
	}
}

func TestDefineDropKeepsSemicolonBeforeASI(t *testing.T) {
	src := `console.log("drop");
(function () {})();
`
	out := apply(t, src, nil, []string{"console"})
	if !strings.Contains(out, "(function () {})()") {
		t.Fatalf("call after drop was removed:\n%s", out)
	}
	if strings.Contains(out, ")()\n(function") {
		t.Fatalf("drop joined two statements:\n%s", out)
	}
}

func TestDefineKeepsCommentAboveDrop(t *testing.T) {
	src := `// license
console.log("drop-me");
keep();
`
	out := apply(t, src, nil, []string{"console"})
	if !strings.Contains(out, "// license") {
		t.Fatalf("leading comment was dropped:\n%s", out)
	}
	if strings.Contains(out, "drop-me") {
		t.Fatalf("call was kept:\n%s", out)
	}
}

func TestDefineFoldsNestedBranches(t *testing.T) {
	src := `if (__DEV__) {
  if (false) {
    dead();
  }
  live();
}
`
	out := apply(t, src, map[string]string{"__DEV__": "true"}, nil)
	if strings.Contains(out, "dead()") || strings.Contains(out, "__DEV__") {
		t.Fatalf("nested fold failed:\n%s", out)
	}
	if !strings.Contains(out, "live()") {
		t.Fatalf("live code was dropped:\n%s", out)
	}
}

func TestValidateDefine(t *testing.T) {
	cases := []struct {
		name    string
		defines map[string]string
		drop    []string
		wantErr string
	}{
		{name: "ok", defines: map[string]string{"__DEV__": "false", "process.env.MODE": `"prod"`}},
		{name: "empty value", defines: map[string]string{"__DEV__": "  "}, wantErr: "value is empty"},
		{name: "bad value", defines: map[string]string{"__DEV__": "this is not js"}, wantErr: "not a JavaScript expression"},
		{name: "statement value", defines: map[string]string{"__DEV__": "1; doThing()"}, wantErr: "not a single JavaScript expression"},
		{name: "bad key", defines: map[string]string{"__DEV-__": "false"}, wantErr: "not an identifier"},
		{name: "reserved key", defines: map[string]string{"class": "false"}, wantErr: "reserved word"},
		{name: "bad drop", drop: []string{"console.log"}, wantErr: "supported values"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.defines, tc.drop)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestApplyDefineDropRejectsBadValue(t *testing.T) {
	_, _, err := ApplyDefineDrop("const a = 1;", map[string]string{"__DEV__": "{"}, nil)
	if err == nil {
		t.Fatal("invalid define value was accepted")
	}
}

func TestDefineScopesAndSyntax(t *testing.T) {
	src := `switch (mode) {
  case 1: {
    const __DEV__ = 1;
    use(__DEV__);
    break;
  }
  default:
    debugger;
}
try {
  risky();
} catch (err) {
  console.log(err);
} finally {
  done();
}
label: for (const key in bag) {
  if (__DEV__) continue label;
}
`
	out := apply(t, src, map[string]string{"__DEV__": "false"}, []string{"console", "debugger"})
	if strings.Contains(out, "console") || strings.Contains(out, "debugger") {
		t.Fatalf("drop failed:\n%s", out)
	}
	if !strings.Contains(out, "use(__DEV__)") {
		t.Fatalf("block-scoped binding was replaced:\n%s", out)
	}
	if strings.Contains(out, "if (false)") || strings.Contains(out, "continue label") {
		t.Fatalf("dead loop body was kept:\n%s", out)
	}
	if !strings.Contains(out, "done()") || !strings.Contains(out, "risky()") {
		t.Fatalf("surrounding code was dropped:\n%s", out)
	}
}

// apply runs the rewrite and fails the test on error.
func apply(t *testing.T, src string, defines map[string]string, drop []string) string {
	t.Helper()
	out, _, err := ApplyDefineDrop(src, defines, drop)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return out
}
