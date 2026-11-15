export type DiffKind = "add" | "del" | "context" | "meta";

export interface DiffLine {
	kind: DiffKind;
	text: string;
}

function splitLines(text: string): string[] {
	const lines = text.replace(/\r\n/g, "\n").split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

function isMeta(line: string): boolean {
	return (
		line.startsWith("+++") ||
		line.startsWith("---") ||
		line.startsWith("@@") ||
		line.startsWith("diff --git") ||
		line.startsWith("index ") ||
		line.startsWith("Binary files") ||
		line.startsWith("*** ")
	);
}

/** parseUnifiedDiff 把 unified diff 文本转成带语义的行，渲染层只负责上色。 */
export function parseUnifiedDiff(text: string): DiffLine[] {
	return splitLines(text).map((line) => {
		if (isMeta(line)) return { kind: "meta", text: line };
		if (line.startsWith("+")) return { kind: "add", text: line.slice(1) };
		if (line.startsWith("-")) return { kind: "del", text: line.slice(1) };
		if (line.startsWith(" ")) return { kind: "context", text: line.slice(1) };
		return { kind: "context", text: line };
	});
}

/**
 * diffFromEdit 用 edit 工具的 oldString/newString 拼出一段可读差异。
 *
 * OpenCode 不保证 edit 的 metadata 里一定带 patch，直接按「整段删除、整段新增」
 * 展示比回退到 JSON 更接近用户看到的实际改动。
 */
export function diffFromEdit(oldText: string, newText: string): DiffLine[] {
	return [
		...splitLines(oldText).map<DiffLine>((text) => ({ kind: "del", text })),
		...splitLines(newText).map<DiffLine>((text) => ({ kind: "add", text })),
	];
}
