import { TextEncoder } from "node:util";
import type { FSAdapter } from "./types.js";

const encoder = new TextEncoder();

// In-memory adapter with the same contract as the Go vfs.FS bridge.
export function createMemoryFS(files: Record<string, string> = {}): FSAdapter & { files(): Record<string, string> } {
	const data = new Map<string, string>(Object.entries(files));
	const normalize = (p: string) => p.replace(/\\/g, "/");

	const adapter: FSAdapter & { files(): Record<string, string> } = {
		caseSensitivity: "sensitive",

		readFile(p) {
			return data.get(normalize(p)) ?? null;
		},

		fileExists(p) {
			return data.has(normalize(p));
		},

		directoryExists(p) {
			const prefix = normalize(p).replace(/\/$/, "") + "/";
			for (const key of data.keys()) {
				if (key.startsWith(prefix)) {
					return true;
				}
			}
			return false;
		},

		entries(p) {
			const prefix = normalize(p).replace(/\/$/, "") + "/";
			const entries: string[] = [];
			const directories = new Set<string>();
			for (const key of data.keys()) {
				if (!key.startsWith(prefix)) {
					continue;
				}
				const rest = key.slice(prefix.length);
				const slash = rest.indexOf("/");
				if (slash < 0) {
					entries.push(rest);
				} else {
					directories.add(rest.slice(0, slash));
				}
			}
			return { files: entries, directories: [...directories], symlinks: {} };
		},

		stat(p) {
			const key = normalize(p);
			const content = data.get(key);
			if (content !== undefined) {
				return {
					name: key.slice(key.lastIndexOf("/") + 1),
					size: encoder.encode(content).length,
					isDirectory: false,
					mtimeMs: 0,
				};
			}
			if (adapter.directoryExists(key)) {
				return {
					name: key.slice(key.lastIndexOf("/") + 1),
					size: 0,
					isDirectory: true,
					mtimeMs: 0,
				};
			}
			return null;
		},

		writeFile(p, content) {
			data.set(normalize(p), content);
		},

		appendFile(p, content) {
			const key = normalize(p);
			data.set(key, (data.get(key) ?? "") + content);
		},

		remove(p) {
			const key = normalize(p);
			for (const existing of [...data.keys()]) {
				if (existing === key || existing.startsWith(key + "/")) {
					data.delete(existing);
				}
			}
		},

		realpath(p) {
			return normalize(p);
		},

		files() {
			return Object.fromEntries(data);
		},
	};

	return adapter;
}
