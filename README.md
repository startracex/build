# build

`build` is a small JavaScript and TypeScript bundler. It follows relative ESM imports, transpiles TypeScript and TSX with [tsgo](https://github.com/startracex/tsgo), and writes one JavaScript file.

This first version is a working CLI. It does not implement Rollup's plugin API, code splitting, CSS, or treeshaking. `bundler.Options.Resolve` is the hook for aliases and, later, package entry points.

## Install

Go 1.27 or newer is required.

```bash
GOPROXY=direct go install github.com/startracex/build/cmd/build@latest
```

`GOPROXY=direct` fetches `github.com/startracex/tsgo` from its repository when the module proxy is stale.

## Usage

```bash
build -o dist/bundle.js src/index.ts
node dist/bundle.js
```

The same bundle can be loaded from Node:

```bash
node -e 'const app = require("./dist/bundle.js"); console.log(app.default)'
```

`--entry` and `--outfile` are the long forms:

```bash
build --entry src/index.ts --outfile dist/bundle.js --jsx react
```

| Flag | Meaning |
| --- | --- |
| `-o`, `--outfile` | Bundle path. Parent directories are created when needed. |
| `--entry` | Entry file. A single positional argument is accepted too. |
| `--jsx` | `react` (default) emits `React.createElement`. `react-jsx` emits the automatic `react/jsx-runtime` import, which stays external. |

## Modules

Relative specifiers (`./` and `../`) are bundled. A specifier such as `./math.js` resolves to `math.ts`, `math.tsx`, or `math.js`, and a specifier with no extension can resolve to `dir/index.ts`.

Default exports are `exports.default`. Named exports stay named properties. Re-exports and `import * as` follow the same CommonJS shape TypeScript emits.

Bare specifiers such as `react` are left as `require("react")` and are loaded by Node when that module runs.

## Develop

```bash
GOPROXY=direct go test ./...
```

The fixture under `bundler/testdata/app` covers TypeScript, TSX, plain JavaScript, default and named exports, re-exports, and a directory index.

## License

Apache-2.0, the same license as tsgo.
