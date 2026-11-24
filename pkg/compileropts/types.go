package compileropts

import (
	"github.com/startracex/tsgo/pkg/core"
	"github.com/startracex/tsgo/pkg/tspath"
)

// Types holds the tsconfig switches that change type checking and inference,
// including the library files a program loads. They change diagnostics and
// declaration types but never the emitted JavaScript text, so they are kept
// apart from Emit and never fingerprint a transpile cache entry.
type Types struct {
	AllowUnreachableCode               core.Tristate
	AllowUnusedLabels                  core.Tristate
	ExactOptionalPropertyTypes         core.Tristate
	NoFallthroughCasesInSwitch         core.Tristate
	NoImplicitAny                      core.Tristate
	NoImplicitOverride                 core.Tristate
	NoImplicitReturns                  core.Tristate
	NoImplicitThis                     core.Tristate
	NoPropertyAccessFromIndexSignature core.Tristate
	NoUncheckedIndexedAccess           core.Tristate
	NoUncheckedSideEffectImports       core.Tristate
	NoUnusedLocals                     core.Tristate
	NoUnusedParameters                 core.Tristate
	SkipDefaultLibCheck                core.Tristate
	Strict                             core.Tristate
	StrictBindCallApply                core.Tristate
	StrictFunctionTypes                core.Tristate
	StrictNullChecks                   core.Tristate
	StrictPropertyInitialization       core.Tristate
	UseUnknownInCatchVariables         core.Tristate
	Lib                                []string
	TypeRoots                          []tspath.RootedDirectoryPath
	Types                              []string
}

// TypesFrom reads the type-checking switches. A nil source yields the zero
// value, which leaves the destination at its defaults.
func TypesFrom(src *core.CompilerOptions) Types {
	if src == nil {
		return Types{}
	}
	return Types{
		Strict:                             src.Strict,
		StrictNullChecks:                   src.StrictNullChecks,
		StrictFunctionTypes:                src.StrictFunctionTypes,
		StrictBindCallApply:                src.StrictBindCallApply,
		StrictPropertyInitialization:       src.StrictPropertyInitialization,
		NoImplicitAny:                      src.NoImplicitAny,
		NoImplicitThis:                     src.NoImplicitThis,
		NoImplicitReturns:                  src.NoImplicitReturns,
		NoImplicitOverride:                 src.NoImplicitOverride,
		NoUncheckedIndexedAccess:           src.NoUncheckedIndexedAccess,
		NoUncheckedSideEffectImports:       src.NoUncheckedSideEffectImports,
		ExactOptionalPropertyTypes:         src.ExactOptionalPropertyTypes,
		UseUnknownInCatchVariables:         src.UseUnknownInCatchVariables,
		NoPropertyAccessFromIndexSignature: src.NoPropertyAccessFromIndexSignature,
		NoFallthroughCasesInSwitch:         src.NoFallthroughCasesInSwitch,
		AllowUnreachableCode:               src.AllowUnreachableCode,
		AllowUnusedLabels:                  src.AllowUnusedLabels,
		NoUnusedLocals:                     src.NoUnusedLocals,
		NoUnusedParameters:                 src.NoUnusedParameters,
		Lib:                                src.Lib,
		Types:                              src.Types,
		TypeRoots:                          src.TypeRoots,
		SkipDefaultLibCheck:                src.SkipDefaultLibCheck,
	}
}

// Apply copies the switches onto dst.
func (o Types) Apply(dst *core.CompilerOptions) {
	dst.Strict = o.Strict
	dst.StrictNullChecks = o.StrictNullChecks
	dst.StrictFunctionTypes = o.StrictFunctionTypes
	dst.StrictBindCallApply = o.StrictBindCallApply
	dst.StrictPropertyInitialization = o.StrictPropertyInitialization
	dst.NoImplicitAny = o.NoImplicitAny
	dst.NoImplicitThis = o.NoImplicitThis
	dst.NoImplicitReturns = o.NoImplicitReturns
	dst.NoImplicitOverride = o.NoImplicitOverride
	dst.NoUncheckedIndexedAccess = o.NoUncheckedIndexedAccess
	dst.NoUncheckedSideEffectImports = o.NoUncheckedSideEffectImports
	dst.ExactOptionalPropertyTypes = o.ExactOptionalPropertyTypes
	dst.UseUnknownInCatchVariables = o.UseUnknownInCatchVariables
	dst.NoPropertyAccessFromIndexSignature = o.NoPropertyAccessFromIndexSignature
	dst.NoFallthroughCasesInSwitch = o.NoFallthroughCasesInSwitch
	dst.AllowUnreachableCode = o.AllowUnreachableCode
	dst.AllowUnusedLabels = o.AllowUnusedLabels
	dst.NoUnusedLocals = o.NoUnusedLocals
	dst.NoUnusedParameters = o.NoUnusedParameters
	dst.Lib = o.Lib
	dst.Types = o.Types
	dst.TypeRoots = o.TypeRoots
	dst.SkipDefaultLibCheck = o.SkipDefaultLibCheck
}
