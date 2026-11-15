import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolDuration, toolOutputText, toolState } from "./state";
import type { ToolView } from "./types";
import { asRecord, asString, trimmedText } from "./values";

const SESSION_ID = /ses_[A-Za-z0-9]{4,}/;

/** childSessionID 只认 OpenCode 的会话 id 前缀，避免把普通文本当成链接。 */
function childSessionID(result: string): string | undefined {
	return result.match(SESSION_ID)?.[0];
}

/**
 * subagentView 同时产出卡片内容和全屏页数据。
 *
 * 子代理的 prompt 和结果通常很长，折叠卡片只给摘要；点开时由手机端弹全屏页，
 * 有 sessionID 时再提供「打开子会话」。
 */
export function subagentView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = asRecord(state.input) ?? {};
	const agent = asString(input.agent) ?? asString(input.subagent_type) ?? "";
	const description = asString(input.description) ?? asString(input.summary) ?? "";
	const prompt = trimmedText(input.prompt);
	const result = trimmedText(toolOutputText(state));
	const metadata = asRecord(state.metadata) ?? {};
	const sessionID =
		asString(metadata.sessionID) ?? asString(metadata.sessionId) ?? childSessionID(result);
	const duration = toolDuration(state);

	view.summary = [agent, description].filter((text) => text !== "").join(" · ");
	view.subagent = {
		agent,
		description,
		prompt,
		result,
		...(sessionID ? { sessionID } : {}),
		...(duration ? { duration } : {}),
		background: input.background === true,
	};
	if (prompt !== "") view.blocks.push({ kind: "text", label: "任务", text: prompt });
	if (result !== "") view.blocks.push({ kind: "code", label: "结果", text: result });
	view.blocks.push(...errorBlock(state));
	return view;
}
