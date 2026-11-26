import nodeFS from "./node-fs.js";
import { createRuntime } from "./runtime.js";
import type { BuildOptions, BuildResult } from "./types.js";

export type {
	BuildOptions,
	BuildResult,
	FSAdapter,
	File,
	Format,
	Mode,
	OutputOptions,
	Plugin,
} from "./types.js";

// Normalize once here so the Go side never has to know about host specifics.
function normalizeOptions(options: BuildOptions) {
	const plugins = options.plugins?.map((plugin) => {
		const transform = plugin.transform;
		return {
			...plugin,
			transform: transform
				? (code: string, id: string) => {
					const result = transform(code, id);
					if (!result || typeof result.map !== "object" || result.map === null) {
						return result;
					}
					return { ...result, map: JSON.stringify(result.map) };
				}
				: undefined,
		};
	});
	return {
		...options,
		plugins,
		fs: options.fs ?? nodeFS,
		cwd: options.cwd ?? process.cwd().replace(/\\/g, "/"),
	};
}

/** Bundles with the wasm build and returns emitted files without writing them. */
export async function build(options: BuildOptions = {}): Promise<BuildResult> {
	const runtime = await createRuntime();
	const result = runtime.build(normalizeOptions(options));
	if ("error" in result) {
		throw new Error(result.error);
	}
	return result;
}

