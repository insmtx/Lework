import type { ContentPart } from "@lework/api";

import { agentView } from "./agent";
import { fileView } from "./files";
import { genericView } from "./generic";
import { searchView } from "./search";
import { shellView } from "./shell";
import type { ToolView } from "./types";
import { asString } from "./values";
import { webView } from "./web";

type Renderer = (part: ContentPart) => ToolView;

/**
 * ROUTES 只做名字到渲染器的映射。
 *
 * 工具名来自 OpenCode，可能新增或改名；未知名字统一走 genericView，保证
 * 任何工具至少能以字段列表而不是整段 JSON 展示。
 */
const ROUTES: Record<string, Renderer> = {
	shell: shellView,
	bash: shellView,
	read: fileView,
	write: fileView,
	edit: fileView,
	patch: fileView,
	glob: searchView,
	grep: searchView,
	webfetch: webView,
	websearch: webView,
	subagent: agentView,
	task: agentView,
	skill: agentView,
	todowrite: agentView,
	question: agentView,
	execute: agentView,
};

/** toolView 把一条工具 part 翻译成手机端的展示模型；非工具 part 返回 null。 */
export function toolView(part: ContentPart): ToolView | null {
	if (part.type !== "tool") return null;
	const name = asString(part.tool);
	const render = name ? ROUTES[name] : undefined;
	return render ? render(part) : genericView(part);
}

export * from "./types";
