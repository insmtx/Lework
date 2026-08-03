/**
 * 路径 C：任务详情群聊续聊。
 *
 * 可以做：先插乐观 user+waiting、AddMessage、启动 GlobalEvents、runtime 轮询兜底开流。
 * 不可以做：CreateInitialMessage、改 layout 路由（由调用方 / effects 负责）。
 */
import { sessionApi } from "../../api/sessionApi";
import type { Attachment, MessageMetadata } from "../../types/chat";
import { mapOutgoingAttachments } from "../../utils/messageAttachments";
import { pollRuntimeStatus } from "../historyLoader";
import { TASK_ROOM_ASSISTANT_START_FALLBACK_MS } from "../messageMerge";
import type { SendPipelineDeps } from "./deps";
import { buildBackendMessageMetadata, extractAssistantIdsFromMetadata } from "./metadata";
import { createOptimisticUserMessage, createWaitingAssistantMessage } from "./optimistic";

/** 任务群聊发送所需的路由身份。 */
export type SendTaskRoomParams = {
	projectId: string;
	taskId: string;
	sessionId: string;
	metadata?: MessageMetadata;
};

/** 发送成功后回给调用方的任务身份（供导航复用）。 */
export type SendTaskRoomResult = {
	project_id: string;
	task_id: string;
	session_id: string;
};

/**
 * 问答路径兜底：只处理「GlobalEvents assistant 一直不来」的终态，绝不在此处开 SessionEvents。
 * SessionEvents 只能由 GlobalEvents assistant 触发；用 responding 去 resume 属于错上加错。
 * GlobalEvents 已接管（streamingMessageId 变化 / 不再 generating）则立即退出。
 */
async function startTaskRoomAssistantFallback(
	deps: SendPipelineDeps,
	sessionId: string,
	assistantMsgId: string,
): Promise<void> {
	try {
		const sessionRes = await sessionApi.get({ session_id: sessionId });
		const baselineMessageCount = sessionRes.data.data?.message_count ?? 0;
		const pollResult = await pollRuntimeStatus(
			sessionId,
			TASK_ROOM_ASSISTANT_START_FALLBACK_MS,
			baselineMessageCount,
		);
		const state = deps.get();
		// 中文注释：GlobalEvents 正常到达后会替换等待占位，此时兜底不再接管。
		if (
			state.activeSessionId !== sessionId ||
			state.streamingMessageId !== assistantMsgId ||
			!state.isGenerating
		) {
			return;
		}
		if (pollResult?.status === "completed") {
			await deps.loadConversationMessages(sessionId, { resumeStream: false });
			deps.finishStream();
			return;
		}
		// responding 或超时：仍未等到 GlobalEvents assistant，标失败，不开放 SessionEvents。
		const current = deps.get().messagesMap[assistantMsgId];
		if (current) {
			deps.updateMessage(assistantMsgId, {
				...current,
				status: "failed",
				statusText:
					pollResult?.status === "responding"
						? "未收到 AI 员工接单通知，请稍后重试。"
						: "AI 员工暂未接单，请稍后重试。",
			});
		}
		deps.finishStream();
	} catch (err) {
		console.error("startTaskRoomAssistantFallback error:", err);
		const state = deps.get();
		if (
			state.activeSessionId !== sessionId ||
			state.streamingMessageId !== assistantMsgId ||
			!state.isGenerating
		) {
			return;
		}
		const current = state.messagesMap[assistantMsgId];
		if (current) {
			deps.updateMessage(assistantMsgId, {
				...current,
				status: "failed",
				statusText: "AI 员工接单状态查询失败，请稍后重试。",
			});
		}
		deps.finishStream();
	}
}

/**
 * 任务群聊发送：乐观 waiting → AddMessage → 等 GlobalEvents；fallback 轮询 runtime。
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
	});

	try {
		void deps.startGlobalEvents();
		await sessionApi.addMessage({
			session_id: params.sessionId,
			role: "user",
			content: trimmed,
			execution_mode: deps.get().executionMode,
			assistant_ids: extractAssistantIdsFromMetadata(params.metadata),
			message_type: "text",
			attachments: mapOutgoingAttachments(attachments),
			metadata: buildBackendMessageMetadata(params.metadata),
		});
	} catch (err) {
		deps.updateMessage(assistantMsg.id, {
			...assistantMsg,
			status: "failed",
			statusText: "消息提交失败，请稍后重试。",
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
	// 中文注释：等不到 GlobalEvents assistant 时收尾为失败；不在兜底里开 SessionEvents。
	void startTaskRoomAssistantFallback(deps, params.sessionId, assistantMsg.id);

	return {
		project_id: params.projectId,
		task_id: params.taskId,
		session_id: params.sessionId,
	};
}
