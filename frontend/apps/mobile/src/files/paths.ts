const WINDOWS_ABSOLUTE = /^[A-Za-z]:[\\/]/;

/** isSafeRelative 判断节点返回的路径能否安全拼到项目根下。 */
export function isSafeRelative(path: string): boolean {
	if (path === "" || path.startsWith("/") || path.startsWith("\\") || WINDOWS_ABSOLUTE.test(path)) {
		return false;
	}
	return !path.split(/[\\/]/).includes("..");
}

/** joinPath 把目录项拼到当前相对路径；目录项可能带尾随斜杠。 */
export function joinPath(base: string, name: string): string {
	const clean = name.replace(/\/+$/, "");
	return base ? `${base}/${clean}` : clean;
}

/** parentPath 返回上一级相对路径，根目录返回空串。 */
export function parentPath(path: string): string {
	const parts = path.split(/[\\/]/).filter(Boolean);
	parts.pop();
	return parts.join("/");
}

/** baseName 取路径最后一段，兼容 Windows 反斜杠。 */
export function baseName(path: string): string {
	const parts = path.split(/[\\/]/).filter(Boolean);
	return parts[parts.length - 1] ?? path;
}
