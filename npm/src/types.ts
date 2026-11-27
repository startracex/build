export type Format = "esm" | "cjs" | "iife" | "umd";
export type Mode = "app" | "lib";

export interface SourceMap {
	version: number;
	sources: string[];
	names?: string[];
	mappings: string;
	sourcesContent?: (string | null)[];
	file?: string;
	sourceRoot?: string;
}

export interface File {
	path: string;
	data: string;
}

export interface FSAdapter {
	caseSensitivity?: "sensitive" | "insensitive";
	readFile(path: string): string | Uint8Array | null | undefined;
	fileExists(path: string): boolean;
	directoryExists(path: string): boolean;
	entries(path: string): {
		files: string[];
		directories: string[];
		symlinks?: Record<string, boolean>;
	} | null | undefined;
	stat(path: string): {
		name?: string;
		size: number;
		isDirectory: boolean;
		mtimeMs: number;
	} | null | undefined;
	writeFile?(path: string, data: string): void;
	appendFile?(path: string, data: string): void;
	remove?(path: string): void;
	realpath?(path: string): string;
}

export interface OutputOptions {
	format?: Format;
	outFile?: string;
	outDir?: string;
	name?: string;
	nameTemplate?: string;
	entryFileNames?: string;
	chunkFileNames?: string;
	assetFileNames?: string;
	declaration?: boolean;
	declarationMap?: boolean;
	sourceMap?: boolean;
	manifest?: boolean;
	publicPath?: string;
	manualChunks?: Record<string, string>;
	depsDir?: string;
}

export interface Plugin {
	name?: string;
	resolve?(source: string, importer: string): { id?: string; external?: boolean; ok?: boolean } | null | undefined;
	load?(id: string): { code: string } | null | undefined;
	transform?(code: string, id: string): { code?: string; map?: SourceMap | string } | null | undefined;
	generateBundle?(files: File[]): File[];
}

export interface BuildOptions {
	entries?: string[];
	mode?: Mode;
	jsx?: string;
	rootDir?: string;
	external?: string[];
	bundleNodeModules?: boolean;
	conditions?: string[];
	define?: Record<string, string>;
	drop?: string[];
	alias?: Record<string, string>;
	cacheDir?: string;
	outputs?: OutputOptions[];
	plugins?: Plugin[];
	/** Defaults to the Node file system adapter. */
	fs?: FSAdapter;
	/** Defaults to process.cwd() with forward slashes. */
	cwd?: string;
}

export interface BuildResult {
	files: File[];
	manifest: Record<string, string>;
	sources: Record<string, string>;
	entry: string;
}

export interface Bridge {
	build(options: BuildOptions): BuildResult | { error: string };
}
