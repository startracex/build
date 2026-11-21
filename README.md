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

App mode is the default. It writes one bundle and inlines the local graph.

```bash
build -o dist/app.mjs src/index.ts
node dist/app.mjs
```

That command also writes `dist/app.mjs.map`. ESM is the default. Load that file as `.mjs`, or from a package whose `"type"` is `"module"`. A browser can load the same file as a module when it does not import Node built-ins. App mode does not write a `.d.ts` file.

```bash
build --format cjs -o dist/app.cjs src/index.ts
node -e 'const app = require("./dist/app.cjs"); console.log(app.default)'
```

Library mode writes one JavaScript file per local module, plus a `.d.ts` and `.d.ts.map` for each TypeScript file. Package imports stay external, and relative imports point at the emitted files. Declaration types come from tsgo's checker, so an exported value keeps its inferred type.

```bash
build --mode lib --outdir dist src/index.ts
```

```bash
build --entry src/index.ts --outfile dist/app.mjs --format esm --jsx react --external left-out
```

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | App mode bundle path. Parent directories are created when needed. |
| `--outdir` | Library mode output directory. |
| `--mode` | `app` (default) or `lib`. |
| `--entry` | Entry file. A single positional argument is accepted too. |
| `--format` | `esm` (default) or `cjs`. |
| `--jsx` | `react` (default) emits `React.createElement`. `react-jsx` emits the automatic JSX runtime and bundles `react/jsx-runtime` when it can be resolved. |
| `--external` | Package or specifier left as an import or require. Repeat the flag to list more than one. Library mode already leaves every bare package external. |
| `--platform` | `node` (default) or `browser`. Selects the matching `package.json` `"exports"` condition. |
| `--sourcemap` | Write source maps. Default is true. Pass `--sourcemap=false` to skip them. |
| `--dts` | Library mode: write `.d.ts` and `.d.ts.map` with tsgo. Default is true. Pass `--dts=false` to skip them. App mode ignores this flag. |

## Modules

Relative specifiers (`./`, `../`) are bundled. `./math.js` can resolve to `math.ts`, and a specifier with no extension can resolve to `dir/index.ts`.

Bare specifiers are loaded from `node_modules` in app mode. Resolution uses tsgo's bundler resolver:

- `package.json` `"exports"`, including `import` and `require` conditions and subpaths
- `"main"` when `"exports"` does not apply
- `"module"` for ESM output when `"exports"` is absent

The app bundle inlines the local graph into one module: real `import` and `export` declarations, no per-file `require` runtime, and no absolute filesystem paths. Node built-ins stay as `import` statements. JSON becomes a `const`. A local `import()` is turned into a promise of that module's exports so the bundle does not expect another file. Side-effect-free exports that nothing reaches are left out. A package whose `package.json` sets `"sideEffects": false` is skipped when it is imported only for its side effects. `export let` bindings stay shared inside the bundle, so updates are visible to other inlined modules.

`--format cjs` uses that same link and assigns the entry exports to `module.exports`.

Library mode keeps each local module in its own file. `import()` stays a dynamic import of the emitted file. Bare package imports are not inlined.

`bundler.Options.Resolve` can replace a specifier with a file. That is the extension point for aliases.

Library declarations are emitted by tsgo after it type-checks the local program. A `const` whose type is inferred keeps that type, for example `number`, instead of `any`. The `.d.ts.map` points from each declaration file back at its source.

App source maps are line-level maps back to the linked source files. Library JavaScript source maps are the maps tsgo emits for each file.

## Not supported

- CSS and other asset imports
- Splitting an app bundle into multiple chunks
- Rollup's plugin API
- Minify, define, and `import.meta.url` rewriting
- A full Rollup treeshake (`/*#__PURE__*/` is not special-cased; other side-effectful initializers are kept)
- Type-checking the whole program (files are parsed, bound, and transpiled per file)

## Develop

```bash
GOPROXY=direct go test ./...
```

## License

Apache-2.0, the same license as tsgo.
