/** asRecord 只在值确实是普通对象时返回它，数组是列表而不是字段表。 */
export function asRecord(value: unknown): Record<string, unknown> | null {
	if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
	return value as Record<string, unknown>;
}

/** asString 把空白字符串当作缺失，避免标题行出现看不见的空值。 */
export function asString(value: unknown): string | undefined {
	if (typeof value !== "string") return undefined;
	const trimmed = value.trim();
	return trimmed === "" ? undefined : trimmed;
}

/** asNumber 兼容部分工具把数字序列化成字符串的 metadata。 */
export function asNumber(value: unknown): number | undefined {
	if (typeof value === "number" && Number.isFinite(value)) return value;
	if (typeof value === "string" && value.trim() !== "") {
		const parsed = Number(value);
		return Number.isFinite(parsed) ? parsed : undefined;
	}
	return undefined;
}

// biome-ignore lint/suspicious/noControlCharactersInRegex: 终端转义序列本身就是控制字符
const ANSI_ESCAPE = /\u001b\][^\u0007]*\u0007|\u001b\[[0-9;?]*[ -/]*[@-~]/g;

/** stripAnsi 去掉终端色码，手机端没有真正的终端来消化它们。 */
export function stripAnsi(text: string): string {
	return text.replace(ANSI_ESCAPE, "");
}

function filePlaceholder(value: Record<string, unknown>): string {
	const name = asString(value.path) ?? asString(value.name) ?? asString(value.filename);
	return name ? `[文件] ${name}` : "[文件]";
}

/**
 * textOf 把工具输出折叠成可读文本。
 *
 * OpenCode 的 `content` 是 `Tool.Content[]`，文本块要拼接，文件块只给占位说明，
 * 不能让调用方再看到一坨 JSON。
 */
export function textOf(value: unknown): string {
	if (value === undefined || value === null) return "";
	if (typeof value === "string") return value;
	if (Array.isArray(value)) {
		return value
			.map((item) => textOf(item))
			.filter((text) => text !== "")
			.join("\n");
	}
	const record = asRecord(value);
	if (!record) return "";
	const text = asString(record.text);
	if (text) return text;
	const output = asString(record.output);
	if (output) return output;
	if (record.type === "file") return filePlaceholder(record);
	return "";
}

/** trimmedText 只裁掉尾部空白，保留代码缩进；非字符串按空值处理。 */
export function trimmedText(value: unknown): string {
	return typeof value === "string" ? value.replace(/\s+$/, "") : "";
}

/** clipText 限制展开区体积，同时明确告诉用户还有更多内容。 */
export function clipText(text: string, limit = 4000): string {
	return text.length > limit ? `${text.slice(0, limit)}\n… 已截断` : text;
}

/** formatDuration 用人类尺度的时长替代原始毫秒数。 */
export function formatDuration(ms: number): string {
	if (ms < 1000) return `${Math.round(ms)}ms`;
	if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
	const minutes = Math.floor(ms / 60_000);
	const seconds = Math.round((ms % 60_000) / 1000);
	return `${minutes}m${seconds}s`;
}

/** firstLine 给折叠摘要取一行，换行会让卡片标题跳动。 */
export function firstLine(text: string): string {
	const line = text.split("\n").find((item) => item.trim() !== "");
	return line?.trim() ?? "";
}
