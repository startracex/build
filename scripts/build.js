// Builds the npm package: the Go wasm bridge, plus ESM modules and their
// declarations emitted straight from npm/src by cmd/build.
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const npm = join(root, "npm");
const out = join(npm, "build");

function run(command, args, options = {}) {
	const result = spawnSync(command, args, {
		cwd: options.cwd ?? root,
		env: { ...process.env, ...(options.env ?? {}) },
		stdio: "inherit",
		shell: false,
	});
	if (result.status !== 0) {
		process.exit(result.status ?? 1);
	}
}

mkdirSync(out, { recursive: true });

run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", join(out, "main.wasm"), "."], {
	cwd: join(root, "cmd", "build-wasm"),
	env: { GOOS: "js", GOARCH: "wasm" },
});
const goroot = spawnSync("go", ["env", "GOROOT"], { encoding: "utf8" }).stdout.trim();
copyFileSync(join(goroot, "lib", "wasm", "wasm_exec.js"), join(out, "wasm_exec.js"));

run("go", ["run", "./cmd/build", "--outdir", out,"--format", "esm",  "-n", "[name].js", "--dts", "--map", "--platform", "node", join(npm, "src")], {
	cwd: root,
});

console.log("built", out);
