import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { webcrypto } from "node:crypto";
import { performance } from "node:perf_hooks";
import type { Bridge } from "./types.js";

// wasm_exec.js is a script, not a module: it installs Go on globalThis. Load it
// through a runtime URL so the bundler keeps it as a separate file beside the
// wasm instead of trying to inline it.
const wasmExecURL = new URL("./wasm_exec.js", import.meta.url);
const wasmURL = new URL("./main.wasm", import.meta.url);

let bridge: Promise<Bridge> | undefined;

export function createRuntime(): Promise<Bridge> {
	if (bridge) {
		return bridge;
	}
	bridge = (async () => {
		const globals = globalThis as Record<string, unknown>;
		globals.fs ??= fs;
		globals.path ??= path;
		globals.crypto ??= webcrypto;
		globals.performance ??= performance;

		await import(wasmExecURL.href);
		const Go = globals.Go as new () => {
			argv: string[];
			env: Record<string, string>;
			run(instance: WebAssembly.Instance): Promise<number>;
			importObject: WebAssembly.Imports;
		};
		if (typeof Go !== "function") {
			throw new Error("build-wasm: wasm_exec.js did not define Go");
		}

		const wasm = fs.readFileSync(wasmURL);
		const go = new Go();
		go.argv = ["build-wasm"];
		go.env = { TMPDIR: os.tmpdir() };
		const module = await WebAssembly.instantiate(wasm, go.importObject);
		// go.run stays pending: this program waits for bridge calls.
		void go.run(module.instance);

		const api = globals["@startracex/build"] as Bridge | undefined;
		if (!api || typeof api.build !== "function") {
			throw new Error("build-wasm: the module did not export its JS bridge");
		}
		return api;
	})();
	return bridge;
}
