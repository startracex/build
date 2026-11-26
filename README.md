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

`[name]` is the entry or module base name, `[path]` is the module path relative to `rootDir` (or the common source directory) without extension, `[ext]` is the extension without the dot, `[type]` is `entry`, `chunk`, or `asset`, and `[format]` is the output format. `[hash]` is a SHA-256 digest of the emitted content; use `[hash:N]` to keep the first `N` characters (default `8`). When two modules render to the same path, the later one gets a `-2`, `-3`, … suffix before the extension. Relative imports are rewritten to the final filenames, including renamed ones. A templated `-o` allows multiple entries and emits one file per module (library mode):

```bash
build -o 'dist/[name].cjs' --format cjs src/
build -o 'dist/[name].[hash:8].js' src/
```

For directory outputs, use the independent `--entry-file-names` flag; the pattern is joined with `--outdir`. It names entry files in both library and split app output:

```bash
build --outdir dist --entry-file-names '[name].[hash:8].js' src/
build --outdir dist --entry-file-names '[path].js' src/
```

Split app output also accepts `--chunk-file-names` for dynamic, shared, and manual chunks, and `--asset-file-names` for `?url` assets and entry CSS. Chunk imports are rewritten relative to the importing file, so a pattern may put entries and chunks in different directories:

```bash
build --mode app --outdir dist \
  --entry-file-names 'entries/[name].[hash:8].[ext]' \
  --chunk-file-names 'js/[name]-[hash:8].[ext]' \
  --asset-file-names 'media/[name]-[hash:8].[ext]' src/index.ts
```

`[type]` expands to `entry`, `chunk`, or `asset`, so one shape can categorize every emitted file:

```bash
build --mode app --outdir dist \
  --entry-file-names '[type]/[name].[ext]' \
  --chunk-file-names '[type]/[name]-[hash:8].[ext]' \
  --asset-file-names '[type]/[name]-[hash:8].[ext]' src/index.ts
# dist/entry/index.js, dist/chunk/lazy-xxxxxxxx.js, dist/asset/note-xxxxxxxx.txt
```

Without a placeholder, `-o` still requires exactly one entry.

### HTML entry

An `.html` file can be used as an entry. It is parsed with `golang.org/x/net/html`: local `<script src>` targets are bundled as JavaScript entries and local `<link rel="stylesheet" href>` targets are copied. The emitted HTML has those URLs rewritten to the generated files; external/data URLs and inline scripts are left untouched.

```bash
build --outdir dist index.html
```

The HTML directory is used as the default root for emitted paths, unless `rootDir` is set in `tsconfig.json` or `--root-dir` is passed.

Both layouts accept an HTML entry:

```bash
# one bundle per script, beside the page: dist/index.html + dist/src/app.js
build --outdir dist --mode app index.html
# one file per module
build --outdir dist --mode lib index.html
```

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
# library (default): one file per module
build --outdir dist src/index.ts
build --outdir dist --format esm --format cjs "src/**/*.ts"
build --outdir dist a.ts b.ts
# app: one bundle per entry, modules shared by several entries → chunks/shared-*.js
build --mode app --outdir dist src/index.ts a.ts b.ts
# app: import() targets → chunks/*.js
build --mode app --outdir dist src/index.ts
```

`--mode` picks the output layout: `app` writes one bundle per entry and inlines package imports, `lib` writes one file per module and leaves package imports external. It is inferred from the output path when omitted: `--outfile` with a single entry is `app`, everything else is `lib`.

Splitting follows the mode. In `app` mode with `--outdir` every `import()` target becomes its own chunk, so the dynamic import stays lazy; modules shared by several entries go to `chunks/shared-*.js`. In `lib` mode nothing is split and `import()` stays a real dynamic import of the emitted file. The one exception is `app` mode with `--outfile`: a single file cannot hold chunks, so the `import()` target is inlined into the bundle.

Passing files on the command line without `-p` ignores `tsconfig.json`. With `-p`, explicit files are used as the entries while compiler options are read from that `tsconfig.json`.

Bundled packages (`--bundle`) are written one file per module, like the rest of the library output. By default they keep a `node_modules/<package>/...` layout, using the innermost `node_modules` so a pnpm store path (`node_modules/.pnpm/<pkg>@<version>/node_modules/<pkg>/...`) does not leak. `--deps-dir` groups them under another directory:

```bash
# dist/vendor/<package>/... instead of dist/node_modules/<package>/...
build --outdir dist --bundle --deps-dir vendor src/index.ts
# flatten every file into one directory (duplicate stems get -2, -3, ...)
build --outdir dist --bundle --entry-file-names '[name].js' src/index.ts
```

Leaving a package external (`--external pkg`) or using `--mode app`, which inlines packages into the entry bundle, avoids per-module dependency files entirely.

### Verbatim JavaScript

A JavaScript module whose emit would only reprint it keeps its original text. Already-minified packages therefore stay minified, comments stay, and tree shaking still applies: only the live statements are kept, and the text around them is untouched.

The original text is used only when the emit changes nothing. A downleveled syntax for the tsconfig `target`, a CommonJS conversion, a JSX transform, or `removeComments` all make the emitted text load-bearing, and then it is used instead.

### tsconfig compiler options

The compiler options that change the output are read from `tsconfig.json`. They are never overridden by a flag.

| Option | Effect |
| --- | --- |
| `module`, `target`, `jsx` | Output format, emit target, and JSX transform. |
| `experimentalDecorators`, `emitDecoratorMetadata` | Legacy decorator transform and `__metadata` calls. |
| `importHelpers`, `noEmitHelpers` | Import the emit helpers from `tslib` instead of inlining them. |
| `useDefineForClassFields` | Native class fields (`true`) or constructor assignment (`false`). |
| `verbatimModuleSyntax` | Keeps import and export statements exactly as written. |
| `jsxFactory`, `jsxFragmentFactory`, `jsxImportSource` | Custom JSX factory, fragment factory, and runtime package. |
| `baseUrl`, `paths` | Resolve aliased specifiers to local files. TypeScript 7 removed `baseUrl`, so it is translated into the equivalent `paths` mappings and a project root for bare specifiers. |
| `moduleResolution` | `node16`, `nodenext`, or `bundler` resolution of `package.json` `"exports"`. |
| `strict` family, `lib`, `types` | Type checking and inference behind the emitted declarations. |
| `customConditions` | Extra `package.json` `"exports"` conditions. |
| `removeComments`, `preserveConstEnums`, `newLine` | Output shape of the emitted JavaScript. |

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Single-file bundle path (repeatable). Supports `[name]`, `[path]`, `[format]`, `[hash]`, and `[hash:N]` placeholders; with a placeholder, several modules emit one file per module. |
| `--outdir` | Directory for per-module or split output (repeatable). |
| `--entry-file-names` | Filename pattern for entry files in `--outdir` output: `[name]`, `[path]`, `[ext]`, `[type]`, `[format]`, `[hash]`, `[hash:N]`. Ignored with `--outfile`. |
| `--chunk-file-names` | Filename pattern for split chunks (dynamic, shared, manual): same placeholders with `[type]` = `chunk`; defaults to the `chunks/` layout. |
| `--asset-file-names` | Filename pattern for emitted `?url` assets and entry CSS: `[name]`, `[path]`, `[ext]`, `[type]` = `asset`, `[hash]`, `[hash:N]`; defaults to the source-relative path with a content hash. |
| `-r`, `--root-dir` | Base directory for paths written under the output dir. Defaults to tsconfig `rootDir`, then the common source directory. |
| arguments | Entry files (including `.html`), directories, or globs. Directories are expanded to supported source files (excluding `node_modules` and `.d.ts`). |
| `-p`, `--project` | `tsconfig.json` file or directory. |
| `--format` | `esm`, `cjs`, `iife`, or `umd`. Repeatable; paired with output paths from the right. |
| `--global-name` | Required for `iife` / `umd`. |
| `--jsx` | `react`, `react-jsx`, `react-jsxdev`, `preserve`, or `react-native`. `preserve` and `react-native` keep JSX syntax in the output. |
| `--external` | Leave a package unbundled (repeatable). |
| `--mode` | Output layout: `app` (one bundle per entry, packages inlined, `import()` split into chunks, no `.d.ts`) or `lib` (one file per module, packages external by default, nothing split). Inferred from the output path when omitted. |
| `--bundle` | Bundle bare package imports in library output instead of leaving them external (alias `--bundle-node-modules`). |
| `--deps-dir` | Directory for bundled packages in library output. Empty keeps `node_modules/<package>/...`; `--deps-dir vendor` writes `vendor/<package>/...`. |
| `--conditions` | Extra `package.json` `"exports"` conditions (repeatable). They are used as given; the resolver never adds a platform-specific condition or changes `moduleResolution`. |
| `--alias` | Specifier prefix mapping `from=to` (repeatable). |
| `--define` | Replace a free reference `KEY=EXPR` (repeatable). Keys are identifiers or dotted chains (`process.env.NODE_ENV`); values must be one JavaScript expression. |
| `--drop` | `console` and/or `debugger` (repeatable). |
| `--map` | Write source maps (alias `--sourcemap`); with `--dts` also writes `.d.ts.map`. |
| `--dts` | Write `.d.ts` for per-module output. |
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

- Single entry + `--outfile`: one linked file (IIFE/UMD need `--global-name`). The `import()` target is inlined into it, since a single file cannot hold chunks.
- App mode + `--outdir`: one bundle per entry; modules shared by several entries go to `chunks/shared-*.js`, and every `import()` target goes to `chunks/*.js`.
- Library mode (`--outdir`) leaves bare package imports external by default; `--bundle` emits package files as regular modules instead.
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

### Define and drop

`--define` and `--drop` are AST edits over each module, not text replacement:

- A define key is replaced only where it is a free reference. Property names (`obj.__DEV__`), strings, comments, declaration names, and identifiers a local binding shadows are left alone. Dotted keys match the whole property chain, and the longest key wins.
- Every value is parsed before the build: an empty value, a statement, or invalid syntax fails with the key name instead of reaching the output. Values that are not a primary expression are parenthesized where they are substituted.
- `drop=console` removes `console.*` calls and drops their arguments' side effects; `drop=debugger` removes `debugger` statements. A call in a statement position disappears, one inside an expression becomes `void 0` (so the surrounding expression still evaluates to `undefined`). A removed statement in a single-statement slot, such as `if (x) console.log(y)`, becomes `{}`, and a deletion that would join the next line through automatic semicolon insertion keeps a `;`.
- Constant branches are folded after the replacement, one nesting level per pass: `if (false)`, `if (true)`, `false && x`, `true || x`. A folded branch keeps its statements at their original offsets, so the body's mappings survive; a multi-statement body stays inside a plain block to preserve indentation and scope.

### Source maps

`--map` composes per-module transpile maps with linker sections; IIFE/UMD wrap offsets are applied. Every define/drop edit stage is applied to the module map before composition: deleted ranges lose their mappings, replaced ranges keep theirs at the replacement position, and later positions shift with the text. Declaration source maps are written when `--dts` and `--map` are both explicitly enabled. When only one of them is passed, or neither is passed, `sourceMap`, `declaration`, and `declarationMap` keep their `tsconfig.json` values.

## Library API

```go
result, err := bundler.Build(ctx, bundler.Options{
  Mode:    bundler.ModeApp,
  Entries: []string{"src/index.ts"},
  Outputs: []bundler.OutputOptions{
    {Format: bundler.FormatESM, OutFile: "dist/index.mjs"},
    {Format: bundler.FormatCJS, OutFile: "dist/index.cjs"},
  },
  Alias:  map[string]string{"@": "./src"},
  Define: map[string]string{"__DEV__": "false"},
  Drop:   []string{"console"},
})
```

`Options.Mode` is required: `bundler.ModeApp` writes one bundle per entry, `bundler.ModeLib` writes one file per module and leaves bare package imports external.

Public entry: `github.com/startracex/build/pkg/bundler`. Sibling packages under `pkg/`: `plugin`, `resolve`, `asset`, `link`, `treeshake`, `define`, `cache`, `wrap`, `sourcemap`, `dts`, `modpath`, `jsedit`, `output`.

The tsconfig compiler options are passed through `Options`, so no flag can override them:

```go
result, err := bundler.Build(context.Background(), bundler.Options{
  Entries:         []string{"src/index.ts"},
  CompilerOptions: compilerOptions, // parsed from tsconfig.json
})
```

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
