import type { ContentPart, Message } from "@lework/api";

/**
 * messageText 优先用聚合文本；部分消息只带 parts，需要现场拼。
 *
 * 只拼接 text part：reasoning 属于执行过程，不进正文气泡。
 */
export function messageText(message: Message): string {
	if (message.text) return message.text;
	if (!message.parts) return "";
	return message.parts
		.map((part) => (part.type === "text" ? (part.text ?? "") : ""))
		.filter((text) => text !== "")
		.join("\n");
}

/** toolParts 返回需要以执行过程展示的工具调用。 */
export function toolParts(message: Message): ContentPart[] {
	if (!message.parts) return [];
	return message.parts.filter((part) => part.type === "tool");
}

/**
 * rendersMarkdown 决定正文是否按 Markdown 渲染。
 *
 * 只有助手的回复是 Markdown；用户输入与合成状态保持纯文本，避免把用户打的
 * 星号、下划线当成格式吞掉。
 */
export function rendersMarkdown(message: Message): boolean {
	return message.role === "assistant";
}

function failedRun(message: Message): boolean {
	return message.outcome === "error" || Boolean(message.error);
}

/**
 * shouldRenderMessage 决定一条消息是否进入列表。
 *
 * idle 是控制面合成的「本轮结束」标记，不是用户或助手的发言；成功时隐藏，
 * 失败时保留，让错误仍然可见。只有 reasoning 的消息同理不占位。
 */
export function shouldRenderMessage(message: Message): boolean {
	if (message.role === "idle") {
		return failedRun(message);
	}
	if (messageText(message) !== "") return true;
	if (toolParts(message).length > 0) return true;
	return Boolean(message.error);
}
