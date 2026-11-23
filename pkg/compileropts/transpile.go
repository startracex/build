package compileropts

import "github.com/startracex/tsgo/pkg/core"

// Transpile holds the compiler options for one transpile: the module kind,
// target, JSX transform, and source map setting the bundler computes, plus the
// tsconfig switches that change the text. The same value produces both the
// compiler options and the cache key, so the key can never omit an option that
// shapes the output.
type Transpile struct {
	Module    core.ModuleKind   `json:"module"`
	Target    core.ScriptTarget `json:"target"`
	Jsx       core.JsxEmit      `json:"jsx"`
	SourceMap core.Tristate     `json:"sourceMap"`
	Emit      Emit              `json:"emit"`
}

// NewTranspile collects everything that affects the emitted text for one
// module of a build.
func NewTranspile(moduleKind core.ModuleKind, target core.ScriptTarget, jsx core.JsxEmit, sourceMap core.Tristate, src *core.CompilerOptions) Transpile {
	return Transpile{
		Module:    moduleKind,
		Target:    target,
		Jsx:       jsx,
		SourceMap: sourceMap,
		Emit:      EmitFrom(src),
	}
}

// CompilerOptions returns a fresh option set for the transpiler. It never
// aliases the tsconfig options, so callers may adjust the result freely.
func (o Transpile) CompilerOptions() *core.CompilerOptions {
	opts := &core.CompilerOptions{
		Module:    o.Module,
		Target:    o.Target,
		Jsx:       o.Jsx,
		SourceMap: o.SourceMap,
	}
	o.Emit.Apply(opts)
	return opts
}

// Key fingerprints the options for the transpile cache.
func (o Transpile) Key() string {
	return key(o)
}
