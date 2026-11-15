import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolOutputText, toolState } from "./state";
import type { ToolField, ToolView } from "./types";
import { asRecord, asString } from "./values";

function inputOf(part: ContentPart): Record<string, unknown> {
	return asRecord(toolState(part).input) ?? {};
}

function filterFields(input: Record<string, unknown>): ToolField[] {
	const fields: ToolField[] = [];
	const path = asString(input.path);
	if (path) fields.push({ label: "路径", value: path });
	const include = asString(input.include);
	if (include) fields.push({ label: "文件", value: include });
	return fields;
}

function outputText(part: ContentPart): string {
	return toolOutputText(toolState(part)).replace(/\s+$/, "");
}

function resultLines(text: string): string[] {
	return text
		.replace(/\r\n/g, "\n")
		.split("\n")
		.map((line) => line.replace(/\s+$/, ""))
		.filter((line) => line.trim() !== "");
}

function completed(part: ContentPart): boolean {
	const status = toolState(part).status;
	return status !== "running" && status !== "streaming";
}

function globView(part: ContentPart): ToolView {
	const view = baseView(part);
	const input = inputOf(part);
	view.summary = asString(input.pattern) ?? "";

	const fields = filterFields(input);
	if (fields.length > 0) view.blocks.push({ kind: "fields", fields });
	const output = outputText(part);
	if (output !== "") {
		view.blocks.push({ kind: "list", items: resultLines(output) });
	} else if (completed(part)) {
		view.blocks.push({ kind: "list", items: [] });
	}
	view.blocks.push(...errorBlock(toolState(part)));
	return view;
}

function grepView(part: ContentPart): ToolView {
	const view = baseView(part);
	const input = inputOf(part);
	view.summary = asString(input.pattern) ?? "";

	const fields = filterFields(input);
	if (fields.length > 0) view.blocks.push({ kind: "fields", fields });
	const output = outputText(part);
	if (output !== "") {
		view.blocks.push({ kind: "code", text: output });
	} else if (completed(part)) {
		view.blocks.push({ kind: "list", items: [] });
	}
	view.blocks.push(...errorBlock(toolState(part)));
	return view;
}

/** searchView 处理 glob 与 grep；二者只差结果的排版方式。 */
export function searchView(part: ContentPart): ToolView {
	return asString(part.tool) === "glob" ? globView(part) : grepView(part);
}
