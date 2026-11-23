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

With no input files, `build` looks for `tsconfig.json` the way `tsc` does. `-p` accepts that file, or a directory that contains it. The build emits one JavaScript file per module unless an output path is given.

```bash
build
build --project tsconfig.json
```

### Single bundle

```bash
build -o dist/app.mjs src/index.ts
```

Both `-o`/`--outfile` and `--outdir` are repeatable. With a single explicit path and several `--format` values, that path is used as a base and the extension is derived per format:

```bash
build -o dist/index.js --format esm --format cjs src/index.ts
```

```bash
build --format iife --global-name App -o dist/app.js src/index.ts
```

`[name]` is the entry or module base name, `[path]` is the module path relative to `rootDir` (or the common source directory) without extension, and `[format]` is the output format. `[hash]` is a SHA-256 digest of the emitted content; use `[hash:N]` to keep the first `N` characters (default `8`). When two modules render to the same path, the later one gets a `-2`, `-3`, … suffix before the extension. Relative imports are rewritten to the final filenames, including renamed ones. A templated `-o` allows multiple entries and emits one file per entry (library mode):

```bash
build -o 'dist/[name].cjs' --format cjs src/
build -o 'dist/[name].[hash:8].js' src/
```

For directory outputs, use the independent `--entry-file-names` flag; the pattern is joined with `--outdir`:

```bash
build --outdir dist --entry-file-names '[name].[hash:8].js' src/
build --outdir dist --entry-file-names '[path].js' src/
```

Without a placeholder, `-o` still requires exactly one entry.

### Output alignment

Output paths and `--format` are paired from the right: the last path takes the last format, and so on. Missing entries are filled in automatically instead of requiring one flag per output:

- Missing formats use `esm`, or the format inferred from tsconfig `module`.
- Missing paths are derived from the fallback base (tsconfig `outDir`, otherwise the first explicit path) with the per-format extension or subdirectory.
- Explicit paths are kept as written whenever more than one path is supplied.

```bash
# dist/a -> esm, dist/b -> cjs
build --outdir dist/a --outdir dist/b --format esm --format cjs src/index.ts

# dist/b -> cjs, dist/a -> esm (missing format filled on the left)
build --outdir dist/a --outdir dist/b --format cjs src/index.ts

# dist/a/cjs (derived), dist/a -> esm, dist/b -> cjs
build --outdir dist/a --outdir dist/b --format cjs --format esm --format cjs src/index.ts
```

`--outfile` and `--outdir` remain mutually exclusive. Single-file output still requires exactly one entry.

### Library / multi-entry / split

```bash
build --outdir dist src/index.ts
build --outdir dist --format esm --format cjs "src/**/*.ts"
build --outdir dist a.ts b.ts
# multi-entry app: shared modules → chunks/shared-*.js
build --outdir dist --splitting src/index.ts
# dynamic import() → chunks/*.js
```

Passing files on the command line without `-p` ignores `tsconfig.json`. With `-p`, explicit files are used as the entries while compiler options are read from that `tsconfig.json`.

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Single-file bundle path (repeatable). Supports `[name]`, `[path]`, `[format]`, `[hash]`, and `[hash:N]` placeholders; with a placeholder, multiple entries emit one file per entry. |
| `--outdir` | Directory for per-module or split output (repeatable). |
| `--entry-file-names` | Filename pattern for `--outdir` outputs: `[name]`, `[path]`, `[format]`, `[hash]`, `[hash:N]`. Ignored with `--outfile`. |
| arguments | Entry files or globs. |
| `-p`, `--project` | `tsconfig.json` file or directory. |
| `--format` | `esm`, `cjs`, `iife`, or `umd`. Repeatable; paired with output paths from the right. |
| `--global-name` | Required for `iife` / `umd`. |
| `--jsx` | `react`, `react-jsx`, `react-jsxdev`, `preserve`, or `react-native`. `preserve` and `react-native` keep JSX syntax in the output. |
| `--external` | Leave a package unbundled (repeatable). |
| `--conditions` | Extra `package.json` `"exports"` conditions (repeatable). |
| `--alias` | Specifier prefix mapping `from=to` (repeatable). |
| `--define` | Replace a free identifier `KEY=VAL` (repeatable); folds `if (false)` / `if (true)` after replace. |
| `--drop` | `console` and/or `debugger` (repeatable). |
| `--map` | Write source maps (alias `--sourcemap`); with `--dts` also writes `.d.ts.map`. |
| `--dts` | Write `.d.ts` for per-module output. |
| `--splitting` | Emit `import()` targets as chunks (requires `--outdir`). |
| `--cache-dir` | Cache transpile results (key includes plugins/define/drop). |
| `--manifest` | Write `manifest.json` for emitted assets. |
| `--public-path` | URL prefix for `?url` / file assets. |

## Modules and assets

| Import | Behavior |
| --- | --- |
| `./x.txt?raw` | Default export is the file text. |
| `./x.png?url` | File copied with content hash in the name; default export is the URL. |
| `./x.css` | App: CSS modules reachable from an entry are concatenated beside that entry’s JS. Lib: files are copied. |
| Other static assets | Treated like `?url` (hashed). |

### App multi-entry and splitting

- Single entry + `--outfile`: one linked file (IIFE/UMD need `--global-name`).
- Multiple entries or `--splitting` require `--outdir`. Shared modules go to `chunks/shared-*.js`; `import()` targets to `chunks/*.js`.
- `Options.ManualChunks` can force modules into named chunks by path substring.
- IIFE/UMD: single entry only.

### Tree shaking

App linking marks live bindings and drops unused pure declarations, including:

- Unused functions, pure inits, pure classes
- `/*#__PURE__*/` / `/*@__PURE__*/` and known-pure `Object.*` callees
- **Property-level**: unused keys on non-escaping object literals (including across `import { obj }` + `obj.keep`)
- `package.json` `"sideEffects": false` or string array / glob
- Shared-chunk exports via `PreserveExports`

Dynamic CJS factories (`__commonJS`) keep the factory body; static CJS shape analysis still shakes unused named bindings. Library mode does not cross-file-shake unused exports.

### Plugins

`Options.Plugins` (`pkg/plugin`): `Resolve`, `Load`, `Transform`, `GenerateBundle`.

Order: **transpile → Transform → define/drop (with branch fold) → link/wrap → source maps → GenerateBundle**.

### Source maps

`--map` (or its alias `--sourcemap`) composes per-module transpile maps with linker sections; IIFE/UMD wrap offsets are applied. Declaration source maps are written when `--dts` and `--map` are both explicitly enabled. When only one of them is passed, or neither is passed, `sourceMap`, `declaration`, and `declarationMap` keep their `tsconfig.json` values.

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

Public entry: `github.com/startracex/build/pkg/bundler`. Sibling packages under `pkg/`: `plugin`, `resolve`, `asset`, `link`, `treeshake`, `define`, `cache`, `wrap`, `sourcemap`, `dts`, `modpath`, `jsedit`, `output`.

## Not supported

- Minify
- Full Rollup plugin surface (`renderChunk`, watch hooks, …)
- `import.meta.url` / `import.meta.env` rewriting
- CSS Modules / PostCSS / style injection into JS (app CSS is concatenated; `?url` assets are content-hashed)
- Dynamic CJS factories (`__commonJS`) keep the whole factory body
- Type-checking a single-file app bundle (library declarations use the checker)
- Module Federation
- Cross-file unused export removal in library mode
- External plugin loading from the CLI (API plugins only)

## Develop

```bash
GOPROXY=direct go test ./...
```

## License

Apache-2.0, the same license as tsgo.
