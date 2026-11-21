# build

`build` bundles a JavaScript or TypeScript program into one file. It resolves imports the way a project does, transpiles TypeScript and TSX with [tsgo](https://github.com/startracex/tsgo), and writes either an ECMAScript module or a CommonJS script.

The library lives in `pkg/bundler`. The command is `cmd/build`.

## Install

Go 1.27 or newer is required.

```bash
GOPROXY=direct go install github.com/startracex/build/cmd/build@latest
```

`GOPROXY=direct` fetches `github.com/startracex/tsgo` from its repository when the module proxy is stale.

## Usage

```bash
build -o dist/app.mjs src/index.ts
node dist/app.mjs
```

ESM is the default. Load that file as `.mjs`, or from a package whose `"type"` is `"module"`. A browser can load the same file as a module when it does not import Node built-ins.

```bash
build --format cjs -o dist/app.cjs src/index.ts
node -e 'const app = require("./dist/app.cjs"); console.log(app.default)'
```

```bash
build --entry src/index.ts --outfile dist/app.mjs --format esm --jsx react
```

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Bundle path. Parent directories are created when needed. |
| `--entry` | Entry file. A single positional argument is accepted too. |
| `--format` | `esm` (default) or `cjs`. |
| `--jsx` | `react` (default) emits `React.createElement`. `react-jsx` emits the automatic JSX runtime and bundles `react/jsx-runtime` when it can be resolved. |

## Modules

Relative specifiers (`./`, `../`) are bundled. `./math.js` can resolve to `math.ts`, and a specifier with no extension can resolve to `dir/index.ts`.

Bare specifiers are loaded from `node_modules`. Resolution uses tsgo's bundler resolver:

- `package.json` `"exports"`, including `import` and `require` conditions and subpaths
- `"main"` when `"exports"` does not apply
- `"module"` for ESM output when `"exports"` is absent

Node built-ins such as `node:path` stay as external `import` statements in an ESM bundle, and as `require` calls in a CommonJS bundle. JSON files become modules. Default and named exports of the entry are re-exported. `export let` updates stay live between modules inside the bundle; the ESM export list itself reads those values when the bundle is evaluated.

`bundler.Options.Resolve` can replace a specifier with a file. That is the extension point for aliases.

## Not supported

- CSS and other asset imports
- Code splitting and multiple output chunks
- Rollup's plugin API
- Treeshaking
- Type-checking the whole program (files are parsed, bound, and transpiled per file)

## Develop

```bash
GOPROXY=direct go test ./...
```

## License

Apache-2.0, the same license as tsgo.
