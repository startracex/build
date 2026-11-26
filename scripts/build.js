// Builds the npm package: the Go wasm bridge and ESM modules with declarations emitted from npm/src by cmd/build.
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync } from "node:fs";
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

run("go", ["run", "./cmd/build", "-p", join(npm, "tsconfig.json"), "-d", out, join(npm, "src")], { cwd: root });

console.log("built", out);
