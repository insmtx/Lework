import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { diffFromEdit, parseUnifiedDiff } from "./diff";
import { type ToolState, toolOutputText, toolState } from "./state";
import type { ToolBlock, ToolView } from "./types";
import { asNumber, asRecord, asString, trimmedText } from "./values";

function inputOf(state: ToolState): Record<string, unknown> {
	return asRecord(state.input) ?? {};
}

function pathOf(input: Record<string, unknown>): string {
	return asString(input.path) ?? asString(input.filePath) ?? "";
}

function outputBlock(state: ToolState, label?: string): ToolBlock[] {
	const output = trimmedText(toolOutputText(state));
	return output === "" ? [] : [{ kind: "code", text: output, ...(label ? { label } : {}) }];
}

function readView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(state);
	view.summary = pathOf(input);

	const offset = asNumber(input.offset);
	const limit = asNumber(input.limit);
	if (offset !== undefined || limit !== undefined) {
		const range: string[] = [];
		if (offset !== undefined) range.push(`从第 ${offset} 行`);
		if (limit !== undefined) range.push(`最多 ${limit} 行`);
		view.blocks.push({ kind: "fields", fields: [{ label: "范围", value: range.join(" · ") }] });
	}
	view.blocks.push(...outputBlock(state, pathOf(input)));
	view.blocks.push(...errorBlock(state));
	return view;
}

function writeView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(state);
	view.summary = pathOf(input);

	const path = pathOf(input);
	const content = trimmedText(input.content);
	if (content !== "")
		view.blocks.push({ kind: "code", text: content, ...(path ? { label: path } : {}) });
	const output = trimmedText(toolOutputText(state));
	if (output !== "" && output !== content) {
		view.blocks.push({ kind: "text", text: output, tone: "muted" });
	}
	view.blocks.push(...errorBlock(state));
	return view;
}

function editView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(state);
	const metadata = asRecord(state.metadata) ?? {};
	view.summary = pathOf(input);

	const path = pathOf(input);
	const patch = trimmedText(metadata.diff) || trimmedText(metadata.patch);
	if (patch.trim() !== "") {
		view.blocks.push({
			kind: "diff",
			lines: parseUnifiedDiff(patch),
			...(path ? { label: path } : {}),
		});
	} else {
		const oldText = trimmedText(input.oldString) || trimmedText(input.old_string);
		const newText = trimmedText(input.newString) || trimmedText(input.new_string);
		if (oldText !== "" || newText !== "") {
			view.blocks.push({
				kind: "diff",
				lines: diffFromEdit(oldText, newText),
				...(path ? { label: path } : {}),
			});
		}
	}
	view.blocks.push(...errorBlock(state));
	return view;
}

const PATCH_KEYS = ["patch", "content", "text", "diff", "input"];

function patchText(input: Record<string, unknown>): string {
	for (const key of PATCH_KEYS) {
		const value = trimmedText(input[key]);
		if (value.trim() !== "") return value;
	}
	const strings = Object.values(input).filter(
		(value): value is string => typeof value === "string" && value.trim() !== "",
	);
	return strings.length === 1 ? strings[0] : "";
}

/** patchTarget 从补丁里找第一个受影响文件，折叠态用它替代「patch」两个字。 */
function patchTarget(text: string): string {
	const marker = text.match(/^\*\*\* (?:Add|Update|Delete) File: (.+)$/m);
	if (marker) return marker[1].trim();
	const plus = text.match(/^\+\+\+ (?:b\/)?(.+?)(?:\s|$)/m);
	return plus ? plus[1].trim() : "";
}

function patchView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const text = patchText(inputOf(state));

	const target = patchTarget(text);
	view.summary = target || "补丁";
	if (text.trim() !== "") {
		view.blocks.push({ kind: "diff", lines: parseUnifiedDiff(text), label: target || "补丁" });
	}
	view.blocks.push(...outputBlock(state));
	view.blocks.push(...errorBlock(state));
	return view;
}

/** fileView 处理 read/write/edit/patch 四个文件工具。 */
export function fileView(part: ContentPart): ToolView {
	switch (asString(part.tool)) {
		case "write":
			return writeView(part);
		case "edit":
			return editView(part);
		case "patch":
			return patchView(part);
		default:
			return readView(part);
	}
}
