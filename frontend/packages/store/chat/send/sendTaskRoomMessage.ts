/**
 * 路径 C：任务详情群聊续聊。
 *
 * 可以做：先插乐观 user+waiting、AddMessage、启动 GlobalEvents、等待 GE assistant 超时兜底。
 * 不可以做：CreateInitialMessage、改 layout 路由（由调用方 / effects 负责）。
 */
import { sessionApi } from "../../api/sessionApi";
import type { Attachment, MessageMetadata } from "../../types/chat";
import { mapOutgoingAttachments } from "../../utils/messageAttachments";
import { waitForGlobalAssistantOrFail } from "./assistantFallback";
import type { SendPipelineDeps } from "./deps";
import { buildBackendMessageMetadata, extractAssistantIdsFromMetadata } from "./metadata";
import { createOptimisticUserMessage, createWaitingAssistantMessage } from "./optimistic";

/** 任务群聊发送所需的路由身份。 */
export type SendTaskRoomParams = {
	projectId: string;
	taskId: string;
	sessionId: string;
	metadata?: MessageMetadata;
	/** 关联到项目的连接器插件 Public ID（仅服务端关联用，不写入消息正文） */
	connectorIds?: string[];
};

/** 发送成功后回给调用方的任务身份（供导航复用）。 */
export type SendTaskRoomResult = {
	project_id: string;
	task_id: string;
	session_id: string;
};

/**
 * 任务群聊发送：乐观 waiting → AddMessage → 等 GlobalEvents；fallback 满窗口等待 assistant。
 */
export async function sendTaskRoomMessage(
	deps: SendPipelineDeps,
	content: string,
	params: SendTaskRoomParams,
	attachments?: Attachment[],
): Promise<SendTaskRoomResult | null> {
	const trimmed = content.trim();
	if (
		(!trimmed && !attachments?.length) ||
		!params.projectId ||
		!params.taskId ||
		!params.sessionId
	) {
		return null;
	}
	if (deps.get().isGenerating) return null;

	const now = Date.now();
	const userMsg = createOptimisticUserMessage(params.sessionId, trimmed, now, {
		attachments,
		metadata: params.metadata,
		status: "sending",
	});
	const assistantMsg = createWaitingAssistantMessage(params.sessionId, now, {
		metadata: params.metadata,
		replyToUser: userMsg,
	});

	// GlobalEvents 用于以持久化数据回填；先展示本地用户消息，确保回复永远不会排在提问之前。
	deps.addMessage(userMsg);
	deps.addMessage(assistantMsg);
	deps.set({
		streamingMessageId: assistantMsg.id,
		isGenerating: true,
		activeSessionId: params.sessionId,
		// 中文注释：新一轮发送解除上一轮超时抑制。
		suppressedReplySessionId: null,
	});

	try {
		void deps.startGlobalEvents();
		await sessionApi.addMessage({
			session_id: params.sessionId,
			role: "user",
			content: trimmed,
			execution_mode: deps.get().executionMode,
			assistant_ids: extractAssistantIdsFromMetadata(params.metadata),
			...(params.connectorIds?.length ? { connector_ids: params.connectorIds } : {}),
			message_type: "text",
			attachments: mapOutgoingAttachments(attachments),
			metadata: buildBackendMessageMetadata(params.metadata),
		});
	} catch (err) {
		deps.updateMessage(assistantMsg.id, {
			...assistantMsg,
			status: "failed",
			statusText: undefined,
			content: "消息提交失败，请稍后重试。",
		});
		deps.finishStream();
		console.error("sendTaskRoomMessage addMessage error:", err);
		return null;
	}

	deps.set({
		// 中文注释：任务群聊按 GlobalEvents -> SessionEvents 强顺序启动，发送成功后等待 GlobalEvents 再拉流。
		streamingMessageId: assistantMsg.id,
		isGenerating: true,
		inputText: "",
		inputAttachments: [],
		activeSessionId: params.sessionId,
	});
	// 中文注释：满 1 分钟仍等不到 GlobalEvents assistant 时写入正文报错；不在兜底里开 SessionEvents。
	void waitForGlobalAssistantOrFail(deps, params.sessionId, assistantMsg.id);

	return {
		project_id: params.projectId,
		task_id: params.taskId,
		session_id: params.sessionId,
	};
}
