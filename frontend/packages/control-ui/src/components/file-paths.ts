export function cleanName(path: string): string {
	return path.endsWith("/") ? path.slice(0, -1) : path;
}

export function isSafeRelative(path: string): boolean {
	return path !== "" && !path.startsWith("/") && !path.split("/").includes("..");
}

export function joinPath(base: string, name: string): string {
	const clean = cleanName(name);
	return base ? `${base}/${clean}` : clean;
}

export function parentPath(path: string): string {
	const parts = path.split("/").filter(Boolean);
	parts.pop();
	return parts.join("/");
}

export function formatBytes(value: number): string {
	if (value < 1024) return `${value} B`;
	if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
	return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}
