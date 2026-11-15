import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolOutputText, toolState } from "./state";
import type { ToolView } from "./types";
import { asRecord, asString } from "./values";

function outputText(part: ContentPart): string {
	return toolOutputText(toolState(part)).replace(/\s+$/, "");
}

function webfetchView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = asRecord(state.input) ?? {};
	view.summary = asString(input.url) ?? "";

	const format = asString(input.format);
	if (format) view.blocks.push({ kind: "fields", fields: [{ label: "格式", value: format }] });
	const output = outputText(part);
	if (output !== "") view.blocks.push({ kind: "code", text: output });
	view.blocks.push(...errorBlock(state));
	return view;
}

function websearchView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = asRecord(state.input) ?? {};
	view.summary = asString(input.query) ?? "";

	const output = outputText(part);
	if (output !== "") view.blocks.push({ kind: "code", text: output });
	view.blocks.push(...errorBlock(state));
	return view;
}

/** webView 处理 webfetch 与 websearch：一个按 URL，一个按查询词。 */
export function webView(part: ContentPart): ToolView {
	return asString(part.tool) === "websearch" ? websearchView(part) : webfetchView(part);
}
