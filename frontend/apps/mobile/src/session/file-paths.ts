/**
 * file-paths 在助手回复的纯文本里找出裸文件路径，交给预览页打开。
 *
 * 规则刻意保守：必须含分隔符且末段带扩展名，避免把 and/or、24/7 这类
 * 普通文本变成链接。首段像域名（example.com/x.md）的一律不认，宁漏不误。
 */

export interface FilePathSpan {
	start: number;
	end: number;
	path: string;
}

// 只认 ASCII 路径字符：中文字符一律当作正文边界，「详见docs/a.md」才不会
// 把「详见」吞进路径。代价是 docs/设计.md 不识别，见设计文档的已知取舍。
const TOKEN = /[A-Za-z0-9._~%+\-/\\:]+/g;
const EXTENSION = /\.[A-Za-z0-9]{1,10}$/;
const WINDOWS_DRIVE = /^[A-Za-z]:[\\/]/;
const PATH_SEPARATOR = /[\\/]/;

function isValidPath(value: string): boolean {
	if (!PATH_SEPARATOR.test(value)) {
		return false;
	}
	// 协议相对 URL（//cdn.example.com/x.png）是外链，不是 UNC。
	if (value.startsWith("//")) {
		return false;
	}
	// 单个前导反斜杠是「当前盘根」，节点侧要求绝对目录，认了也打不开。
	if (value.startsWith("\\") && !value.startsWith("\\\\")) {
		return false;
	}
	// ~ 只有 shell 会展开，会话目录解析不出来，别做成必然失败的链接。
	if (value.startsWith("~")) {
		return false;
	}
	const absolute = value.startsWith("/") || value.startsWith("\\") || WINDOWS_DRIVE.test(value);
	if (!absolute && !value.startsWith(".")) {
		const first = value.split(PATH_SEPARATOR).find(Boolean) ?? "";
		// 相对路径首段带点视为域名；v1.2/README.md 这类会因此漏掉，有意取舍。
		if (first.includes(".")) {
			return false;
		}
	}
	const last = value.split(PATH_SEPARATOR).pop() ?? "";
	return EXTENSION.test(last);
}

/**
 * 按冒号切开 token，保留盘符冒号（C:\），并丢弃像 scheme 的剩余部分
 * （https://、见C:\... 里的路径）。返回值是相对 token 的下标。
 */
function splitToken(value: string): { offset: number; piece: string }[] {
	const pieces: { offset: number; piece: string }[] = [];
	let start = 0;
	for (let i = 0; i < value.length; i++) {
		if (value[i] !== ":") continue;
		const drive = i === 1 && /[A-Za-z]/.test(value[0]) && PATH_SEPARATOR.test(value[2] ?? "");
		if (drive) continue;
		pieces.push({ offset: start, piece: value.slice(start, i) });
		// 冒号前是字母/数字时，后面属于 scheme 或紧贴路径的前缀（见C:\...），
		// 整段丢弃，避免把 https://x.md 或 \repo\x.md 当成项目文件。
		if (/[A-Za-z0-9]/.test(value[i - 1] ?? "")) {
			return pieces;
		}
		start = i + 1;
	}
	pieces.push({ offset: start, piece: value.slice(start) });
	return pieces;
}

/**
 * findFilePathSpans 返回文本里可点击的文件路径片段。
 *
 * start/end 是 UTF-16 下标，path === content.slice(start, end)；行尾句号在
 * 计算 end 之前已去掉，因此不会进入路径。
 */
export function findFilePathSpans(content: string): FilePathSpan[] {
	const spans: FilePathSpan[] = [];
	for (const match of content.matchAll(TOKEN)) {
		const base = match.index ?? 0;
		for (const { offset, piece } of splitToken(match[0])) {
			const path = piece.replace(/\.+$/, "");
			if (!path || !isValidPath(path)) continue;
			spans.push({ start: base + offset, end: base + offset + path.length, path });
		}
	}
	return spans;
}
