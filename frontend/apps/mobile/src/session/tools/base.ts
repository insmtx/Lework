import type { ContentPart } from "@lework/api";

import {
	rawToolState,
	type ToolState,
	toolErrorText,
	toolState,
	toolStatus,
	toolTone,
} from "./state";
import type { ToolBlock, ToolView } from "./types";
import { asString } from "./values";

/** baseView 收拢所有工具共有的状态字段，视图只需要补充摘要和内容块。 */
export function baseView(part: ContentPart): ToolView {
	const state = toolState(part);
	return {
		tool: asString(part.tool) ?? "tool",
		status: toolStatus(state),
		tone: toolTone(state),
		summary: "",
		blocks: [],
		raw: rawToolState(part),
	};
}

/** errorBlock 把结构化错误放进展开区末尾，而不是混进正文。 */
export function errorBlock(state: ToolState): ToolBlock[] {
	const error = toolErrorText(state);
	return error === "" ? [] : [{ kind: "text", text: error, tone: "error" }];
}
