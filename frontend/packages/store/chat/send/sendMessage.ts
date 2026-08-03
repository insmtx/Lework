/**
 * 路径 A：纯 /chat 发送。
 *
 * 可以做：必要时 CreateSession、AddMessage、插乐观 user+空 assistant、immediate 开 SessionEvents。
 * 不可以做：碰 GlobalEvents、任务路由跳转。
 */
import { sessionApi } from "../../api/sessionApi";
import type { Attachment, MessageMetadata } from "../../types/chat";
import { mapOutgoingAttachments } from "../../utils/messageAttachments";
import type { SendPipelineDeps } from "./deps";
import { buildBackendMessageMetadata, extractAssistantIdsFromMetadata } from "./metadata";
import { createEmptyAssistantMessage, createOptimisticUserMessage } from "./optimistic";

/**
 * 纯 chat 发送。无 activeSession 时先建 chat 会话并写入侧栏。
 * AddMessage 成功后才插乐观气泡并 immediate 开流（失败不插本地消息）。
 */
export async function sendMessage(
	deps: SendPipelineDeps,
	content: string,
	attachments?: Attachment[],
	metadata?: MessageMetadata,
): Promise<boolean> {
	// 仅上传附件而无文字时后端会报错，必须要求有文本内容
	if (!content.trim()) return false;

	const state = deps.get();
	// 中文注释：生成中禁止再次发送，避免新的请求把上一条流式响应直接顶掉。
	if (state.isGenerating) return false;
	let { activeSessionId } = state;

	if (!activeSessionId) {
		try {
			const res = await sessionApi.create({ type: "chat", title: "新会话" });
			const session = res.data.data;
			if (!session) return false;
			activeSessionId = session.session_id;
			deps.effects.prependChatConversation({
				id: session.session_id,
				title: session.title || "未命名会话",
				sessionId: session.session_id,
				type: session.type,
				status: session.status,
				createdAt: new Date(session.created_at).getTime(),
				updatedAt: new Date(session.updated_at).getTime(),
			});
		} catch (err) {
			console.error("Auto-create conversation error:", err);
			return false;
		}
	}

	try {
		await sessionApi.addMessage({
			session_id: activeSessionId,
			role: "user",
			content,
			execution_mode: state.executionMode,
			assistant_ids: extractAssistantIdsFromMetadata(metadata),
			message_type: "text",
			attachments: mapOutgoingAttachments(attachments),
			metadata: buildBackendMessageMetadata(metadata),
		});
	} catch (err) {
		console.error("sendMessage addMessage error:", err);
		return false;
	}

	const now = Date.now();
	const userMsg = createOptimisticUserMessage(activeSessionId, content, now, {
		attachments,
		metadata,
	});
	const assistantMsg = createEmptyAssistantMessage(activeSessionId, now, userMsg);

	deps.addMessage(userMsg);
	deps.addMessage(assistantMsg);
	deps.set({
		streamingMessageId: assistantMsg.id,
		isGenerating: true,
		inputText: "",
		inputAttachments: [],
	});

	void deps.startSessionStream(activeSessionId, assistantMsg.id);
	return true;
}
