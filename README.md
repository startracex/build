# build

`build` bundles a JavaScript or TypeScript program. It resolves imports the way a project does, transpiles TypeScript and TSX with [tsgo](https://github.com/startracex/tsgo), and writes ECMAScript modules or CommonJS. Declarations and source maps are written by default. Declaration files come from tsgo.

The library lives in `pkg/bundler`. The command is `cmd/build`.

## Install

Go 1.27 or newer is required.

```bash
GOPROXY=direct go install github.com/startracex/build/cmd/build@latest
```

`GOPROXY=direct` fetches `github.com/startracex/tsgo` from its repository when the module proxy is stale.

## Usage

With no input files, `build` looks for `tsconfig.json` the way `tsc` does: the working directory first, then each parent directory. `-p` accepts that file, or a directory that contains it. The build emits one JavaScript file per module. `module`, `jsx`, `outDir`, `rootDir`, `declaration`, and `sourceMap` come from that file. Each TypeScript file gets a `.d.ts` and `.d.ts.map` unless `declaration` is false.

```bash
build
build --project tsconfig.json
```

`--outfile` with one entry writes a single bundle and a source map. It does not write a `.d.ts`. More than one entry with `--outfile` is an error.

```bash
build -o dist/app.mjs src/index.ts
node dist/app.mjs
```

```bash
build --format cjs -o dist/app.cjs src/index.ts
node -e 'const app = require("./dist/app.cjs"); console.log(app.default)'
```

`--outdir` accepts one entry, several entries, or a glob. Package imports stay external. Relative imports point at the emitted files.

```bash
build --outdir dist src/index.ts
build --outdir dist "src/**/*.ts"
```

Passing files on the command line does not load `tsconfig.json`. If one is found, the command stops, matching `tsc`. `--ignoreConfig` skips that check and uses the files you passed.

```bash
build --ignoreConfig -o dist/app.mjs src/index.ts --external react
```

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Single-file bundle path. Requires exactly one entry. |
| `--outdir` | Directory for per-module output. Accepts one or more entries. |
| arguments | Entry files or globs. `**` matches across directories. A leading `!` excludes. |
| `-p`, `--project` | `tsconfig.json` file, or a directory containing one. Cannot be combined with input files. |
| `--ignoreConfig` | Use command-line files even when a `tsconfig.json` is found above the working directory. |
| `--format` | `esm` or `cjs`. The default is `tsconfig` `module`, or `esm`. |
| `--jsx` | `react` emits `React.createElement`. `react-jsx` emits the automatic JSX runtime. The default is `tsconfig` `jsx`, or `react`. |
| `--external` | Package name left as an import or require. Repeat the flag to list more than one. Per-module output already leaves every bare package external. |
| `--platform` | `node` (default) or `browser`. Selects the matching `package.json` `"exports"` condition. |
| `--sourcemap` | Write source maps. The default follows `tsconfig`, or true. Pass `--sourcemap=false` to skip them. |
| `--dts` | Write `.d.ts` and `.d.ts.map` for per-module output. The default follows `tsconfig` `declaration`, or true. Single-file output does not write declarations. |

## Modules

Relative specifiers (`./`, `../`) are bundled. `./math.js` can resolve to `math.ts`, and a specifier with no extension can resolve to `dir/index.ts`.

Bare specifiers are loaded from `node_modules` when the output is one file. Resolution uses tsgo's bundler resolver:

- `package.json` `"exports"`, including `import` and `require` conditions and subpaths
- `"main"` when `"exports"` does not apply
- `"module"` for ESM output when `"exports"` is absent

A single file inlines the local graph into one module: real `import` and `export` declarations, no per-file `require` runtime, and no absolute filesystem paths. Node built-ins stay as `import` statements. JSON becomes a `const`. A local `import()` is turned into a promise of that module's exports so the bundle does not expect another file. Side-effect-free exports that nothing reaches are left out. A package whose `package.json` sets `"sideEffects": false` is skipped when it is imported only for its side effects. `export let` bindings stay shared inside the bundle, so updates are visible to other inlined modules.

`--format cjs` uses that same link and assigns the entry exports to `module.exports`.

Per-module output keeps each local module in its own file. `import()` stays a dynamic import of the emitted file. Bare package imports are not inlined.

`bundler.Options.Resolve` can replace a specifier with a file. That is the extension point for aliases.

Library declarations are emitted by tsgo after it type-checks the local program. A `const` whose type is inferred keeps that type, for example `number`, instead of `any`. The `.d.ts.map` points from each declaration file back at its source.

Single-file source maps are line-level maps back to the linked source files. Per-module JavaScript source maps are the maps tsgo emits for each file.

## Not supported

- CSS and other asset imports
- Splitting one bundle into multiple chunks
- Rollup's plugin API
- Minify, define, and `import.meta.url` rewriting
- A full Rollup treeshake (`/*#__PURE__*/` is not special-cased; other side-effectful initializers are kept)
- Type-checking a single-file bundle (those files are parsed, bound, and transpiled per file; per-module declarations use the checker)

## Develop

```bash
GOPROXY=direct go test ./...
```

## License

Apache-2.0, the same license as tsgo.
