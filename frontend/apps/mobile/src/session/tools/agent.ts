import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolOutputText, toolState } from "./state";
import { subagentView } from "./subagent";
import type { ToolView } from "./types";
import { asRecord, asString, firstLine, trimmedText } from "./values";

function inputOf(part: ContentPart): Record<string, unknown> {
	return asRecord(toolState(part).input) ?? {};
}

function skillView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(part);
	view.summary = asString(input.id) ?? asString(input.name) ?? "";

	const text = trimmedText(toolOutputText(state));
	if (text !== "") view.blocks.push({ kind: "text", label: "内容", text });
	view.blocks.push(...errorBlock(state));
	return view;
}

interface TodoItem {
	text: string;
	done: boolean;
}

function todoItems(input: Record<string, unknown>): TodoItem[] {
	if (!Array.isArray(input.todos)) return [];
	return input.todos
		.map((item) => asRecord(item))
		.filter((item): item is Record<string, unknown> => item !== null)
		.map((item) => ({
			text: asString(item.content) ?? asString(item.text) ?? "",
			done: item.status === "completed" || item.done === true,
		}))
		.filter((item) => item.text !== "");
}

function todoView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const items = todoItems(inputOf(part));
	const done = items.filter((item) => item.done).length;

	view.summary = items.length > 0 ? `${done}/${items.length} 完成` : "";
	if (items.length > 0) view.blocks.push({ kind: "checklist", items });
	view.blocks.push(...errorBlock(state));
	return view;
}

function questionText(question: Record<string, unknown>): string {
	const text = asString(question.question) ?? "";
	const options = Array.isArray(question.options) ? question.options : [];
	const labels = options
		.map((option) => asString(asRecord(option)?.label ?? option))
		.filter((label): label is string => label !== undefined);
	return [text, ...labels.map((label) => `• ${label}`)].filter((line) => line !== "").join("\n");
}

function questionView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(part);
	const questions = Array.isArray(input.questions) ? input.questions : [];

	for (const raw of questions) {
		const question = asRecord(raw);
		if (!question) continue;
		const text = questionText(question);
		if (text === "") continue;
		view.blocks.push({ kind: "text", label: asString(question.header) ?? "问题", text });
	}
	view.summary = firstLine(view.blocks[0]?.kind === "text" ? view.blocks[0].text : "");
	view.blocks.push(...errorBlock(state));
	return view;
}

function executeView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = inputOf(part);
	const code = trimmedText(input.code) || trimmedText(input.script);
	view.summary = firstLine(code);

	if (code !== "") view.blocks.push({ kind: "code", label: "代码", text: code });
	const output = trimmedText(toolOutputText(state));
	if (output !== "") view.blocks.push({ kind: "code", label: "输出", text: output });
	view.blocks.push(...errorBlock(state));
	return view;
}

/** agentView 处理与 Agent 交互相关的工具，纯展示，不承担执行语义。 */
export function agentView(part: ContentPart): ToolView {
	switch (asString(part.tool)) {
		case "subagent":
		case "task":
			return subagentView(part);
		case "todowrite":
			return todoView(part);
		case "question":
			return questionView(part);
		case "execute":
			return executeView(part);
		case "skill":
			return skillView(part);
		default:
			return baseView(part);
	}
}
