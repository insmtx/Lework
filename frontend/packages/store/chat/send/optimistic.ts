/**
 * 发送路径统一的乐观消息构造。
 *
 * 可以做：构造 msg-user-* / msg-assistant-waiting-* / 空 assistant 占位。
 * 不可以做：写 store、发 HTTP、开 SSE。
 */
import type { Attachment, Message, MessageMetadata } from "../../types/chat";
import { mapComposerAttachments } from "../../utils/messageAttachments";
import { buildReplyToFromMessage } from "../messageReducer";

/** 乐观用户消息可选字段。 */
export type OptimisticUserOptions = {
	attachments?: Attachment[];
	metadata?: MessageMetadata;
	/** 任务场景用 sending；纯 chat 成功后插入可不带 status */
	status?: Message["status"];
};

/**
 * 构造乐观用户气泡（id 形如 msg-user-{now}）。
 * now 由调用方传入，保证同一次发送里 user/assistant 时间戳成对。
 */
export function createOptimisticUserMessage(
	sessionId: string,
	content: string,
	now: number,
	options?: OptimisticUserOptions,
): Message {
	return {
		id: `msg-user-${now}`,
		conversationId: sessionId,
		role: "user",
		content,
		timestamp: now,
		...(options?.status ? { status: options.status } : {}),
		attachments: mapComposerAttachments(options?.attachments),
		metadata: options?.metadata,
	};
}

/**
 * 构造任务场景 waiting 占位（msg-assistant-waiting-*）。
 * 在 GlobalEvents 回填真 assistant 前展示「正在分配」，并透传 @ 队友 metadata。
 */
export function createWaitingAssistantMessage(
	sessionId: string,
	now: number,
	options?: {
		metadata?: MessageMetadata;
		replyToUser?: Message;
		statusText?: string;
	},
): Message {
	return {
		id: `msg-assistant-waiting-${now}`,
		conversationId: sessionId,
		role: "assistant",
		content: "",
		timestamp: now + 100,
		status: "waiting",
		statusText: options?.statusText ?? "正在提交问题并分配 AI 员工...",
		// 中文注释：等待 GlobalEvents 回填前，先用本次输入的 @ 队友信息稳定展示头像和名称。
		metadata: options?.metadata,
		replyTo: options?.replyToUser ? buildReplyToFromMessage(options.replyToUser) : undefined,
		author: {
			id: "pending-assistant",
			name: "Lework",
			type: "assistant",
		},
	};
}

/**
 * 构造纯 chat 空 assistant 占位（msg-assistant-*，无 waiting 文案）。
 * 配合 StreamOpenStrategy=immediate，AddMessage 成功后立刻开 SessionEvents。
 */
export function createEmptyAssistantMessage(
	sessionId: string,
	now: number,
	replyToUser: Message,
): Message {
	return {
		id: `msg-assistant-${now}`,
		conversationId: sessionId,
		role: "assistant",
		content: "",
		timestamp: now + 100,
		replyTo: buildReplyToFromMessage(replyToUser),
	};
}
