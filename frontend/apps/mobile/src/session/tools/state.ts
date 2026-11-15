import type { ContentPart } from "@lework/api";

import type { ToolTone } from "./types";
import { asRecord, asString, formatDuration, textOf } from "./values";

/** ToolState 对应 OpenCode `Session.Message.ToolState.*` 的公共字段。 */
export interface ToolState {
	status?: string;
	title?: string;
	input?: unknown;
	output?: unknown;
	content?: unknown;
	error?: unknown;
	metadata?: unknown;
	time?: { start?: number; end?: number };
}

export function toolState(part: ContentPart): ToolState {
	return asRecord(part.state) ?? {};
}

export function toolStatus(state: ToolState): string {
	return asString(state.status) ?? "running";
}

const RUNNING_STATUSES = new Set(["running", "streaming", "pending"]);

export function toolTone(state: ToolState): ToolTone {
	if (toolStatus(state) === "error" || toolErrorText(state) !== "") return "error";
	if (RUNNING_STATUSES.has(toolStatus(state))) return "running";
	return "ok";
}

/** toolOutputText 统一 output / content / metadata.output 三种携带输出的方式。 */
export function toolOutputText(state: ToolState): string {
	const output = textOf(state.output);
	if (output !== "") return output;
	const content = textOf(state.content);
	if (content !== "") return content;
	const metadata = asRecord(state.metadata);
	return textOf(metadata?.output);
}

/** toolErrorText 兼容字符串错误和 OpenCode 的 StructuredError。 */
export function toolErrorText(state: ToolState): string {
	const error = asRecord(state.error);
	if (error) return asString(error.message) ?? "";
	return asString(state.error) ?? "";
}

export function toolDuration(state: ToolState): string | undefined {
	const start = state.time?.start;
	const end = state.time?.end;
	if (typeof start !== "number" || typeof end !== "number" || end < start) return undefined;
	return formatDuration(end - start);
}

/** rawToolState 保留完整 state，展开区的「原始数据」指向它。 */
export function rawToolState(part: ContentPart): string {
	const state = asRecord(part.state);
	if (!state || Object.keys(state).length === 0) return "";
	try {
		return JSON.stringify(state, null, 2);
	} catch {
		return "";
	}
}
