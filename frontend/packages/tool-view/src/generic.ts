import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolOutputText, toolState } from "./state";
import type { ToolField, ToolView } from "./types";
import { asRecord, asString, firstLine } from "./values";

const SUMMARY_KEYS = [
	"description",
	"title",
	"command",
	"path",
	"filePath",
	"url",
	"query",
	"pattern",
	"id",
	"text",
];

const MAX_FIELDS = 12;

function fieldValue(value: unknown): string {
	if (typeof value === "string") return value;
	if (typeof value === "number" || typeof value === "boolean") return String(value);
	if (value === null || value === undefined) return "";
	try {
		return JSON.stringify(value);
	} catch {
		return String(value);
	}
}

function summaryOf(input: Record<string, unknown>, output: string): string {
	for (const key of SUMMARY_KEYS) {
		const value = asString(input[key]);
		if (value) return value;
	}
	for (const value of Object.values(input)) {
		const text = asString(value);
		if (text) return text;
	}
	return firstLine(output);
}

function inputFields(input: Record<string, unknown>): ToolField[] {
	return Object.entries(input)
		.map(([label, value]) => ({ label, value: fieldValue(value) }))
		.filter((field) => field.value !== "")
		.slice(0, MAX_FIELDS);
}

/** genericView 是没有专属渲染器的工具（多为 MCP 工具）的兜底排版。 */
export function genericView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = asRecord(state.input) ?? {};
	const output = toolOutputText(state).replace(/\s+$/, "");

	view.summary = summaryOf(input, output);
	const fields = inputFields(input);
	if (fields.length > 0) view.blocks.push({ kind: "fields", fields });
	if (output !== "") view.blocks.push({ kind: "code", text: output });
	view.blocks.push(...errorBlock(state));
	return view;
}
