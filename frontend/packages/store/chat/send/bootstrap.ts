/**
 * 新建任务跳转前的本地 bootstrap（无 HTTP）。
 *
 * 可以做：替换本地时间线为乐观 user + waiting assistant，打 pendingBootstrap 标记，
 * drain GlobalEvents 缓冲。
 * 不可以做：调 CreateInitialMessage / AddMessage、开 SessionEvents。
 */
import type { Attachment, Message, MessageMetadata } from "../../types/chat";
import { mapComposerAttachments } from "../../utils/messageAttachments";
import type { SendPipelineDeps } from "./deps";
import { createOptimisticUserMessage, createWaitingAssistantMessage } from "./optimistic";

/** bootstrap 可选附件与展示 metadata。 */
export type BootstrapNewTaskOptions = {
	attachments?: Attachment[];
	metadata?: MessageMetadata;
};

/**
 * 新建任务跳转任务详情前写入等待占位。
 * 若带附件则同步展示乐观用户消息；pendingBootstrapSessionId 阻止 historyLoader 冲掉乐观态。
 * 调用方必须在 navigateToTaskDetail 之前执行本函数。
 */
export function bootstrapNewTaskSession(
	deps: SendPipelineDeps,
	sessionId: string,
	content: string,
	options?: BootstrapNewTaskOptions,
): void {
	const trimmed = content.trim();
	const optimisticAttachments = mapComposerAttachments(options?.attachments);
	if (!sessionId || (!trimmed && !optimisticAttachments?.length)) return;

	const now = Date.now();
	const userMsg: Message | null =
		trimmed || optimisticAttachments?.length
			? createOptimisticUserMessage(sessionId, trimmed, now, {
					attachments: options?.attachments,
					metadata: options?.metadata,
					status: "sending",
				})
			: null;
	const assistantMsg = createWaitingAssistantMessage(sessionId, now, {
		metadata: options?.metadata,
		replyToUser: userMsg ?? undefined,
	});

	const messagesMap: Record<string, Message> = {
		[assistantMsg.id]: assistantMsg,
	};
	const messageIds = [assistantMsg.id];
	if (userMsg) {
		messagesMap[userMsg.id] = userMsg;
		messageIds.unshift(userMsg.id);
	}

	deps.set({
		activeSessionId: sessionId,
		messagesMap,
		messageIds,
		streamingMessageId: assistantMsg.id,
		isGenerating: true,
		pendingBootstrapSessionId: sessionId,
		// 中文注释：新一轮发送解除上一轮超时抑制，允许再次等待 GE assistant。
		suppressedReplySessionId: null,
	});
	deps.drainGlobalEvents(sessionId);
}
