package compileropts

import "github.com/startracex/tsgo/pkg/core"

// Syntax holds the tsconfig switches that shape both transpiled JavaScript and declaration output: decorators, JSX, and module syntax.
type Syntax struct {
	ExperimentalDecorators  core.Tristate `json:"experimentalDecorators"`
	EmitDecoratorMetadata   core.Tristate `json:"emitDecoratorMetadata"`
	UseDefineForClassFields core.Tristate `json:"useDefineForClassFields"`
	VerbatimModuleSyntax    core.Tristate `json:"verbatimModuleSyntax"`
	PreserveConstEnums      core.Tristate `json:"preserveConstEnums"`
	JsxFactory              string        `json:"jsxFactory"`
	JsxFragmentFactory      string        `json:"jsxFragmentFactory"`
	JsxImportSource         string        `json:"jsxImportSource"`
	ReactNamespace          string        `json:"reactNamespace"`
}

// SyntaxFrom reads the shared switches; a nil source yields the zero value.
func SyntaxFrom(src *core.CompilerOptions) Syntax {
	if src == nil {
		return Syntax{}
	}
	return Syntax{
		ExperimentalDecorators:  src.ExperimentalDecorators,
		EmitDecoratorMetadata:   src.EmitDecoratorMetadata,
		UseDefineForClassFields: src.UseDefineForClassFields,
		VerbatimModuleSyntax:    src.VerbatimModuleSyntax,
		PreserveConstEnums:      src.PreserveConstEnums,
		JsxFactory:              src.JsxFactory,
		JsxFragmentFactory:      src.JsxFragmentFactory,
		JsxImportSource:         src.JsxImportSource,
		ReactNamespace:          src.ReactNamespace,
	}
}

// Apply copies the switches onto dst.
func (o Syntax) Apply(dst *core.CompilerOptions) {
	dst.ExperimentalDecorators = o.ExperimentalDecorators
	dst.EmitDecoratorMetadata = o.EmitDecoratorMetadata
	dst.UseDefineForClassFields = o.UseDefineForClassFields
	dst.VerbatimModuleSyntax = o.VerbatimModuleSyntax
	dst.PreserveConstEnums = o.PreserveConstEnums
	dst.JsxFactory = o.JsxFactory
	dst.JsxFragmentFactory = o.JsxFragmentFactory
	dst.JsxImportSource = o.JsxImportSource
	dst.ReactNamespace = o.ReactNamespace
}

// JavaScript holds the switches that only change JavaScript text; a declaration carries no implementation, so these never reach declaration emit.
type JavaScript struct {
	IsolatedModules core.Tristate    `json:"isolatedModules"`
	ImportHelpers   core.Tristate    `json:"importHelpers"`
	NoEmitHelpers   core.Tristate    `json:"noEmitHelpers"`
	RemoveComments  core.Tristate    `json:"removeComments"`
	NewLine         core.NewLineKind `json:"newLine"`
}

// JavaScriptFrom reads the JavaScript-only switches.
func JavaScriptFrom(src *core.CompilerOptions) JavaScript {
	if src == nil {
		return JavaScript{}
	}
	return JavaScript{
		IsolatedModules: src.IsolatedModules,
		ImportHelpers:   src.ImportHelpers,
		NoEmitHelpers:   src.NoEmitHelpers,
		RemoveComments:  src.RemoveComments,
		NewLine:         src.NewLine,
	}
}

// Apply copies the switches onto dst, leaving NewLine at its default when the tsconfig did not choose one.
func (o JavaScript) Apply(dst *core.CompilerOptions) {
	dst.IsolatedModules = o.IsolatedModules
	dst.ImportHelpers = o.ImportHelpers
	dst.NoEmitHelpers = o.NoEmitHelpers
	dst.RemoveComments = o.RemoveComments
	if o.NewLine != core.NewLineKindNone {
		dst.NewLine = o.NewLine
	}
}

// Emit holds every tsconfig switch that changes transpiled JavaScript.
type Emit struct {
	JavaScript JavaScript `json:"javascript"`
	Syntax     Syntax     `json:"syntax"`
}

// EmitFrom reads every switch that changes emitted JavaScript.
func EmitFrom(src *core.CompilerOptions) Emit {
	return Emit{Syntax: SyntaxFrom(src), JavaScript: JavaScriptFrom(src)}
}

// Apply copies the switches onto dst.
func (o Emit) Apply(dst *core.CompilerOptions) {
	o.Syntax.Apply(dst)
	o.JavaScript.Apply(dst)
}

// Key fingerprints the switches for the transpile cache.
func (o Emit) Key() string {
	return key(o)
}
