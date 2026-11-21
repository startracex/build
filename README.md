# build

`build` bundles a JavaScript or TypeScript program. It resolves imports the way a project does, transpiles TypeScript and TSX with [tsgo](https://github.com/startracex/tsgo), and writes ECMAScript modules, CommonJS, IIFE, or UMD. Source maps and declarations follow TypeScript: they are off unless `tsconfig.json` or a flag turns them on.

The library lives in `pkg/bundler`. The command is `cmd/build`.

## Install

Go 1.27 or newer is required.

```bash
GOPROXY=direct go install github.com/startracex/build/cmd/build@latest
```

`GOPROXY=direct` fetches `github.com/startracex/tsgo` from its repository when the module proxy is stale.

## Usage

With no input files, `build` looks for `tsconfig.json` the way `tsc` does. `-p` accepts that file, or a directory that contains it. The build emits one JavaScript file per module unless `--outfile` is set.

```bash
build
build --project tsconfig.json
```

### Single bundle

```bash
build -o dist/app.mjs src/index.ts
```

Repeat `--format` for Rollup-style multiple outputs from one input (ESM + CJS dual package):

```bash
build -o dist/index.js --format esm --format cjs src/index.ts
# writes dist/index.mjs and dist/index.cjs
```

```bash
build --format iife --global-name App -o dist/app.js src/index.ts
```

### Library / split output

```bash
build --outdir dist src/index.ts
build --outdir dist --format esm --format cjs "src/**/*.ts"
# writes under dist/esm and dist/cjs
```

```bash
build --outdir dist --splitting src/index.ts
# dynamic import() targets become files under dist/chunks/
```

Passing files on the command line ignores `tsconfig.json`. `-p` cannot be combined with input files.

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Single-file bundle path. With several `--format` values, the extension is derived per format. |
| `--outdir` | Directory for per-module or split output. |
| arguments | Entry files or globs. |
| `-p`, `--project` | `tsconfig.json` file or directory. |
| `--format` | `esm`, `cjs`, `iife`, or `umd`. Repeatable. |
| `--global-name` | Global variable for `iife` / `umd`. |
| `--jsx` | `react` or `react-jsx`. |
| `--external` | Leave a package unbundled (repeatable). |
| `--conditions` | Extra `package.json` `"exports"` conditions (repeatable). |
| `--alias` | Specifier prefix mapping `from=to` (repeatable), e.g. `@=./src`. |
| `--define` | Replace a free identifier `KEY=VAL` (repeatable). |
| `--drop` | `console` and/or `debugger` (repeatable). |
| `--sourcemap` | Write JavaScript source maps. |
| `--dts` | Write `.d.ts` for per-module output. |
| `--declarationMap` | Write `.d.ts.map` when declarations are on. |
| `--splitting` | Emit `import()` targets as chunks (requires `--outdir`). |
| `--cache-dir` | Cache transpile results on disk. |
| `--manifest` | Write `manifest.json` for emitted assets. |
| `--public-path` | URL prefix for `?url` / file assets. |

## Modules and assets

Relative and package imports resolve as before (`exports`, `main`, `module`). Conditions: ESM uses `default`/`module`/`import`; CJS uses `default`/`module`/`require`.

| Import | Behavior |
| --- | --- |
| `./x.txt?raw` | Default export is the file text. |
| `./x.png?url` | File is copied; default export is a URL string. |
| `./x.css` | CSS is emitted beside the JS; the JS module is a side-effect import. |
| Other static assets | Treated like `?url`. |

`define` / `drop` run on transpiled JS before linking. Alias longest-prefix match runs before the package resolver.

### App multi-entry and splitting

- Single entry + `--outfile`: one linked file (optional IIFE/UMD with `--global-name`).
- Multiple entries or `--splitting` require `--outdir`. The graph is shared: each entry is one file; modules used by ≥2 entries (or multiple async roots) go to `chunks/shared-*.js`; `import()` targets become `chunks/*.js`.
- IIFE/UMD: **single entry only**; `--global-name` is required.

### Tree shaking (app bundle)

Linking marks live bindings from entry (and shared-chunk) exports and drops unused pure declarations. Supported:

- Unused functions, vars with pure inits, and classes without impure `extends` / static initializers
- `/*#__PURE__*/` / `/*@__PURE__*/` annotated calls and `new` (including unused statement forms)
- A small set of known-pure `Object.*` callees when arguments are pure
- `package.json` `"sideEffects": false` or a string array / glob (`["*.css"]`)

Library `--outdir` still emits per file and does not cross-file-shake unused exports. Dynamic CJS (`__commonJS`) keeps the whole factory.

### Plugins

`Options.Plugins` (`pkg/plugin`) exposes four hooks, between esbuild and Rollup:

| Hook | When |
| --- | --- |
| `Resolve` | Before the package resolver (first match wins) |
| `Load` | Before reading a file (virtual modules) |
| `Transform` | After transpile, before `define`/`drop` (chained) |
| `GenerateBundle` | After emit, before `Build` returns (chained) |

Pipeline: **transpile → Transform → define/drop → link/wrap → source maps → GenerateBundle**.

### Source maps

With `--sourcemap`, app bundles compose transpile maps with linker `// file` sections (column mappings when transpile maps exist).

## Library API

```go
result, err := bundler.Build(ctx, bundler.Options{
  Entry: "src/index.ts",
  Outputs: []bundler.OutputOptions{
    {Format: bundler.FormatESM, OutFile: "dist/index.mjs"},
    {Format: bundler.FormatCJS, OutFile: "dist/index.cjs"},
  },
  Alias:  map[string]string{"@": "./src"},
  Define: map[string]string{"__DEV__": "false"},
  Drop:   []string{"console"},
})
```

Public entry: `github.com/startracex/build/pkg/bundler`. Sibling packages: `plugin`, `resolve`, `asset`, `link`, `treeshake`, `define`, `cache`, `wrap`, `sourcemap`, `dts`, `modpath`, `jsedit`, `output`.

## Not supported

- Minify
- Full Rollup plugin surface (`renderChunk`, watch hooks, …)
- `import.meta.url` rewriting
- Property-level tree shaking / `if (false)` DCE
- Cross-file unused export removal in library mode
- Type-checking a single-file app bundle (library declarations use the checker)
- Module Federation

## Develop

```bash
GOPROXY=direct go test ./...
```

## License

Apache-2.0, the same license as tsgo.
