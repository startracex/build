// Package compileropts projects the tsconfig compiler options onto the subsets
// each stage of a build reads.
//
// A projection is defined once and used both to build the tsgo compiler options
// and, where a stage caches its result, to fingerprint them. That keeps the two
// from drifting and makes the split explicit:
//
//   - Emit changes the transpiled JavaScript. Syntax carries the switches that
//     also shape declaration output; JavaScript carries the text-only ones.
//   - Types changes type checking and inference. It never changes JavaScript,
//     so it must not invalidate a transpile cache entry.
//   - Resolution decides which file a module specifier points at.
//
// Transpile composes the options the bundler derives for one emit.
package compileropts
