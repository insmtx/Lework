/**
 * file-links 只负责把助手回复里的 Markdown 链接翻译成「节点上的哪个文件」。
 * 真正的路径校验在节点侧（workspace 限制 + symlink 检查），这里不做拦截。
 */

export interface FileLinkTarget {
	directory: string;
	path: string;
}

const WINDOWS_ABSOLUTE = /^[A-Za-z]:[\\/]/;
const SCHEME = /^[A-Za-z][A-Za-z0-9+.-]*:/;

function isAbsolutePath(value: string): boolean {
	return value.startsWith("/") || value.startsWith("\\\\") || WINDOWS_ABSOLUTE.test(value);
}

function decode(value: string): string {
	try {
		return decodeURIComponent(value);
	} catch {
		return value;
	}
}

/**
 * resolveFileLink 把链接 href 解析成 (directory, path)。返回 null 表示交给
 * 系统浏览器，包括外链、锚点，以及拿不到会话目录的相对路径。
 */
export function resolveFileLink(href: string, sessionDirectory: string): FileLinkTarget | null {
	const trimmed = href.trim();
	if (!trimmed || trimmed.startsWith("#")) {
		return null;
	}

	const hash = trimmed.indexOf("#");
	const withoutFragment = hash >= 0 ? trimmed.slice(0, hash) : trimmed;
	const query = withoutFragment.indexOf("?");
	const withoutQuery = query >= 0 ? withoutFragment.slice(0, query) : withoutFragment;
	let link = decode(withoutQuery);
	if (!link) {
		return null;
	}

	if (/^file:/i.test(link)) {
		link = link.slice("file:".length).replace(/^\/\//, "");
	} else if (link.startsWith("//")) {
		// 协议相对 URL（//cdn.example.com/x.png）是外链，不是 UNC 路径。
		return null;
	} else if (!isAbsolutePath(link) && SCHEME.test(link)) {
		return null;
	}
	if (!link) {
		return null;
	}

	if (isAbsolutePath(link)) {
		const split = Math.max(link.lastIndexOf("/"), link.lastIndexOf("\\"));
		const name = link.slice(split + 1);
		if (!name) {
			return null;
		}
		return { directory: link.slice(0, split + 1), path: name };
	}

	if (!sessionDirectory) {
		return null;
	}
	return { directory: sessionDirectory, path: link.replace(/^\.\//, "") };
}

/**
 * fileBaseDirectory 返回 Markdown 文件里相对链接的基准目录：文件在子目录时
 * 相对的是同一子目录，而不是会话根目录。
 */
export function fileBaseDirectory(directory: string, path: string): string {
	if (!directory) {
		return directory;
	}
	const split = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
	if (split < 0) {
		return directory;
	}
	const sep = path[split];
	const base = directory.endsWith("/") || directory.endsWith("\\") ? directory : directory + sep;
	return base + path.slice(0, split + 1);
}
