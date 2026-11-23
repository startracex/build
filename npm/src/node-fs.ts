import fs from "node:fs";
import path from "node:path";
import type { FSAdapter } from "./types.js";

// The Go bridge passes slash-separated rooted paths. Map them back to host
// paths here; on Windows a leading slash belongs to the current drive.
const windows = process.platform === "win32";

function toHost(p: string): string {
	const slash = p.replace(/\\/g, "/");
	if (slash.startsWith("/") && windows) {
		return path.resolve(process.cwd().slice(0, 2), slash.slice(1));
	}
	return path.resolve(slash);
}

function toVirtual(p: string): string {
	return p.replace(/\\/g, "/");
}

const nodeFS: FSAdapter = {
	caseSensitivity: windows ? "insensitive" : "sensitive",

	readFile(p) {
		try {
			// A Buffer is a Uint8Array; the Go side copies bytes out of it, so
			// binary assets survive the bridge.
			return fs.readFileSync(toHost(p));
		} catch {
			return null;
		}
	},

	fileExists(p) {
		try {
			return fs.statSync(toHost(p)).isFile();
		} catch {
			return false;
		}
	},

	directoryExists(p) {
		try {
			return fs.statSync(toHost(p)).isDirectory();
		} catch {
			return false;
		}
	},

	entries(p) {
		try {
			const files: string[] = [];
			const directories: string[] = [];
			const symlinks: Record<string, boolean> = {};
			for (const entry of fs.readdirSync(toHost(p), { withFileTypes: true })) {
				if (entry.isSymbolicLink()) {
					symlinks[entry.name] = true;
				}
				if (entry.isDirectory()) {
					directories.push(entry.name);
				} else {
					files.push(entry.name);
				}
			}
			return { files, directories, symlinks };
		} catch {
			return null;
		}
	},

	stat(p) {
		try {
			const info = fs.statSync(toHost(p));
			return {
				name: path.basename(p),
				size: info.size,
				isDirectory: info.isDirectory(),
				mtimeMs: info.mtimeMs,
			};
		} catch {
			return null;
		}
	},

	writeFile(p, data) {
		const host = toHost(p);
		fs.mkdirSync(path.dirname(host), { recursive: true });
		fs.writeFileSync(host, data);
	},

	appendFile(p, data) {
		const host = toHost(p);
		fs.mkdirSync(path.dirname(host), { recursive: true });
		fs.appendFileSync(host, data);
	},

	remove(p) {
		fs.rmSync(toHost(p), { recursive: true, force: true });
	},

	realpath(p) {
		try {
			return toVirtual(fs.realpathSync(toHost(p)));
		} catch {
			return p;
		}
	},
};

export default nodeFS;
export { nodeFS };
