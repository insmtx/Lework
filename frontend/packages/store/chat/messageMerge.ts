/**
 * 乐观消息与落库消息的合并、占位判定，以及 GlobalEvents 用户消息相关纯函数。
 *
 * 可以做：判断乐观/占位 ID、optimistic ↔ persisted merge、构造/插入 GlobalEvents human 消息。
 * 不可以做：发 HTTP、开/关 SSE、读写 Zustand（除接收 ChatState 做纯计算）、导航。
 */
import type {
	BackendMessageAttachment,
	BackendMessageMetadata,
	BackendWorkTitleUpdatedPayload,
	SSEMessageEvent,
} from "../api/types";
import type { Message, MessageAttachment, MessageRole } from "../types/chat";
import { parseOptionalTimestamp } from "../utils/format";
import { buildMessageMetadata } from "../utils/messageMetrics";
import {
	buildReplyToFromMessage,
	isRecord,
	mapBackendAttachment,
	resolveReplyToFromRunId,
} from "./messageReducer";
import type { ChatState } from "./state";

/** GlobalEvents `message.created` 的 payload 形状（人/助手共用字段）。 */
export type BackendGlobalMessagePayload = {
	id?: string | number;
	message_id?: string | number;
	sender_type?: "human" | "assistant" | string;
	sender_uin?: number;
	sender_name?: string;
	content?: string;
	message_type?: string;
	sequence?: number;
	attachments?: BackendMessageAttachment[];
	created_at?: string;
	run_id?: string;
	assistant_id?: string;
	assistant_name?: string;
	metadata?: BackendMessageMetadata;
};

/** GlobalEvents 可能携带的 payload 联合类型。 */
export type BackendGlobalPayload = BackendGlobalMessagePayload | BackendWorkTitleUpdatedPayload;

/** GlobalEvents 单条事件的基础结构。 */
export type BackendGlobalEventBase<
	TType extends string = string,
	TPayload = BackendGlobalPayload,
> = {
	type?: TType;
	project_id?: string;
	task_id?: string;
	session_id?: string;
	timestamp?: number;
	payload?: TPayload;
	data?: TPayload;
};

/** 全局「消息已创建」事件。 */
export type BackendGlobalMessageCreatedEvent = BackendGlobalEventBase<
	"message.created",
	BackendGlobalMessagePayload
>;

/** 全局「工作标题更新」事件。 */
export type BackendGlobalWorkTitleUpdatedEvent = BackendGlobalEventBase<
	"work.title.updated",
	BackendWorkTitleUpdatedPayload
>;

/** GlobalEvents 解析后的事件联合。 */
export type BackendGlobalEvent =
	| BackendGlobalMessageCreatedEvent
	| BackendGlobalWorkTitleUpdatedEvent
	| BackendGlobalEventBase;

/**
 * 将 GlobalEvents 附件列表映射为前端 MessageAttachment。
 * 过滤掉缺少 file_upload_id 的无效项。
 */
function mapGlobalMessageAttachments(
	attachments: BackendMessageAttachment[] | undefined,
	messageCreatedAt?: number,
): MessageAttachment[] | undefined {
	const mapped = attachments
		?.map((attachment) => mapBackendAttachment(attachment, messageCreatedAt))
		.filter((attachment): attachment is MessageAttachment => attachment !== undefined);
	return mapped?.length ? mapped : undefined;
}

/**
 * 判断是否为前端本地乐观消息。
 * 约定：id 以 `msg-user-` / `msg-assistant-` 开头（含 waiting/resume/poll 等变体）。
 */
export function isOptimisticMessage(message: Message): boolean {
	return message.id.startsWith("msg-user-") || message.id.startsWith("msg-assistant-");
}

/**
 * 归一化消息正文，用于乐观消息与落库/推送消息的内容比对。
 * 去掉首尾空白并把连续空白压成单空格，避免 markdown 细微差异导致配对失败。
 */
export function normalizedMessageContent(message: Message): string {
	return message.content.trim().replace(/\s+/g, " ");
}

/**
 * 判断本地 user 消息是否与 GlobalEvents 回推的 human 消息是同一条（回声）。
 * 仅用于合并「自己发出的」乐观消息，避免时间线上出现重复用户气泡。
 *
 * 注意：不能按同文案/同昵称/无 author 去匹配已落库消息，否则群聊里队友提问会被
 * 合并进旧气泡，表现为底部只见模型回复、结束后历史回拉才出现提问。
 */
export function isGlobalUserEchoMessage(message: Message | undefined, incoming: Message): boolean {
	if (!message || message.conversationId !== incoming.conversationId || message.role !== "user") {
		return false;
	}
	if (normalizedMessageContent(message) !== normalizedMessageContent(incoming)) return false;

	// 中文注释：只认本地乐观发送或 current-user 标记；他人/历史消息即便文案相同也必须新插一条。
	return isOptimisticMessage(message) || message.author?.id === "current-user";
}

/**
 * 判断 messageId 是否为任务场景的流式占位 assistant
 *（waiting / resume / poll 三种本地临时 id）。
 */
function isAssistantStreamPlaceholderId(messageId: string): boolean {
	return (
		messageId.startsWith("msg-assistant-waiting-") ||
		messageId.startsWith("msg-assistant-resume-") ||
		messageId.startsWith("msg-assistant-poll-")
	);
}

/**
 * 判断是否为任务群聊中的 assistant 占位气泡。
 * 即使已进入 streaming（提前开了 SessionEvents），仍应被后续 GlobalEvents 的真 assistant 替换。
 */
export function isTaskRoomAssistantPlaceholder(
	message: Message | undefined,
	sessionId: string,
): boolean {
	return (
		message?.conversationId === sessionId &&
		message.role === "assistant" &&
		isAssistantStreamPlaceholderId(message.id) &&
		(message.status === "waiting" || message.status === "streaming" || message.status === undefined)
	);
}

/**
 * GlobalEvents 晚到并替换占位 assistant 时，把占位上已收到的流式片段继承到新消息，避免内容闪断。
 */
export function inheritStreamingAssistantState(target: Message, source?: Message): Message {
	if (!source) return target;
	return {
		...target,
		replyTo: target.replyTo ?? source.replyTo,
		content: source.content || target.content,
		toolCalls: source.toolCalls ?? target.toolCalls,
		todos: source.todos ?? target.todos,
		processSteps: source.processSteps ?? target.processSteps,
		approvals: source.approvals ?? target.approvals,
		questions: source.questions ?? target.questions,
		artifacts: source.artifacts ?? target.artifacts,
		metadata: source.metadata ?? target.metadata,
		usage: source.usage ?? target.usage,
		status: source.status === "streaming" ? "streaming" : target.status,
		statusText: source.status === "streaming" ? undefined : target.statusText,
	};
}

/** SessionEvents 长时间无事件时，用拉历史兜底的等待毫秒数。 */
export const SESSION_EVENTS_IDLE_FALLBACK_MS = 10_000;

/** SessionEvents 发起后一直无法成功建连（onOpen）时的超时毫秒数。 */
export const SESSION_EVENTS_CONNECT_FALLBACK_MS = 60_000;

/** 发出后等不到 GlobalEvents assistant 时的完整等待窗口（不因 responding 提前失败）。 */
export const TASK_ROOM_ASSISTANT_START_FALLBACK_MS = 60_000;

/** assistant 已接单、等待 SessionEvents 正文时的占位文案。 */
export const ASSISTANT_SESSION_EVENTS_WAITING_TEXT = "AI 员工已接单，正在生成回复...";

/** GlobalEvents 只回 human、始终无 assistant 时的超时报错正文。 */
export const ASSISTANT_GLOBAL_EVENTS_TIMEOUT_TEXT =
	"回复超时：系统已收到你的提问，但 AI 员工未能成功接单。请稍后重试。";

/** SessionEvents 一直无法成功建连时的超时报错正文。 */
export const ASSISTANT_SESSION_EVENTS_TIMEOUT_TEXT = "回复超时：无法建立实时回复连接，请稍后重试。";

/**
 * 判断是否为前端本地写入的回复超时报错气泡（waiting/resume/poll 或超时正文）。
 * 离开再进时应丢弃，避免历史里堆叠多条客户端超时残留。
 */
export function isClientReplyTimeoutMessage(message: Message | undefined): boolean {
	if (!message || message.role !== "assistant" || message.status !== "failed") return false;
	if (isAssistantStreamPlaceholderId(message.id)) return true;
	const content = message.content.trim();
	return (
		content === ASSISTANT_GLOBAL_EVENTS_TIMEOUT_TEXT ||
		content === ASSISTANT_SESSION_EVENTS_TIMEOUT_TEXT ||
		// 中文注释：兼容旧版带「刷新页面」的超时报错正文，离开再进时同样清掉。
		(isOptimisticMessage(message) && content.startsWith("回复超时："))
	);
}

/**
 * 构造「等待 SessionEvents」的 assistant 占位消息。
 * 用于进页回放、GlobalEvents 已到但 SSE 尚未出内容等场景。
 * 可透传 replyTo/author/runId，避免离开再进后「回复某某」引用条短暂消失。
 */
export function createAssistantSessionEventsWaitingMessage(
	sessionId: string,
	id: string,
	timestamp = Date.now(),
	options?: {
		replyTo?: Message["replyTo"];
		author?: Message["author"];
		runId?: string;
	},
): Message {
	return {
		id,
		conversationId: sessionId,
		role: "assistant",
		content: "",
		timestamp,
		status: "waiting",
		// 刷新恢复 SSE 回放时保留明确等待态，避免只显示空白生成中占位。
		statusText: ASSISTANT_SESSION_EVENTS_WAITING_TEXT,
		...(options?.runId ? { runId: options.runId } : {}),
		...(options?.replyTo ? { replyTo: options.replyTo } : {}),
		author: options?.author ?? {
			id: "pending-assistant",
			name: "Lework",
			type: "assistant",
		},
	};
}

/**
 * 为 resume/poll 占位解析 replyTo / author / runId。
 * 群聊下优先沿用本地 streaming 的 replyTo，其次用 runId（req_<userMsgId>）精确绑到提问者，
 * 最后才回落到时间线末条 user，避免把「回复某某」指到后来插队发言的队友。
 */
export function resolveSessionEventsWaitingContext(
	sessionId: string,
	localMessages: Message[],
	timelineMessages: Message[],
): {
	replyTo?: Message["replyTo"];
	author?: Message["author"];
	runId?: string;
} {
	const localAssistant = [...localMessages]
		.reverse()
		.find(
			(message) =>
				message.conversationId === sessionId &&
				message.role === "assistant" &&
				(message.status === "waiting" ||
					message.status === "streaming" ||
					message.status === undefined) &&
				Boolean(message.replyTo || message.runId || message.author),
		);

	const messagesForLookup = Object.fromEntries(
		[...timelineMessages, ...localMessages].map((message) => [message.id, message]),
	);
	const replyToFromRunId = resolveReplyToFromRunId(
		localAssistant?.runId,
		messagesForLookup,
		sessionId,
	);

	const lastUser =
		[...timelineMessages].reverse().find((message) => message.role === "user") ??
		[...localMessages]
			.reverse()
			.find((message) => message.conversationId === sessionId && message.role === "user");

	return {
		replyTo: localAssistant?.replyTo ?? replyToFromRunId ?? buildReplyToFromMessage(lastUser),
		author: localAssistant?.author,
		runId: localAssistant?.runId,
	};
}

/**
 * 生成用于内容配对的 merge key：`角色:归一化正文`。
 * 无正文时返回 undefined（仅靠附件等场景不走内容配对）。
 */
function messageMergeKey(message: Message): string | undefined {
	const content = normalizedMessageContent(message);
	if (!content) return undefined;
	return `${message.role}:${content}`;
}

/**
 * 统计列表中与 target 同 merge key 的消息条数（可截断到 targetIndex）。
 * 用于判断「同内容第几次出现」，避免多轮相同文案配对错位。
 */
function countMatchingMessages(messages: Message[], target: Message, targetIndex?: number): number {
	const key = messageMergeKey(target);
	if (!key) return 0;

	let count = 0;
	const end = targetIndex ?? messages.length - 1;
	for (let index = 0; index <= end; index += 1) {
		const message = messages[index];
		if (message && messageMergeKey(message) === key) {
			count += 1;
		}
	}
	return count;
}

/**
 * 统计列表中指定角色的出现次数（可截断到 targetIndex）。
 * 乐观消息与落库消息 id 不同时，按角色出现顺序兜底配对。
 */
function countMessagesByRole(messages: Message[], role: MessageRole, targetIndex?: number): number {
	let count = 0;
	const end = targetIndex ?? messages.length - 1;
	for (let index = 0; index <= end; index += 1) {
		if (messages[index]?.role === role) {
			count += 1;
		}
	}
	return count;
}

/**
 * 按角色出现顺序取第 occurrence 条消息（1-based）。
 * 与 countMessagesByRole 配套，用于 optimistic ↔ persisted 配对。
 */
function findMessageByRoleOccurrence(
	messages: Message[],
	role: MessageRole,
	occurrence: number,
): Message | undefined {
	if (occurrence <= 0) return undefined;

	let count = 0;
	for (const message of messages) {
		if (message.role !== role) continue;
		count += 1;
		if (count === occurrence) {
			return message;
		}
	}
	return undefined;
}

/**
 * 在落库列表中查找与本地消息对应的那一条：先精确 id，乐观消息再按角色出现序兜底。
 */
function findPersistedMessageMatch(
	persistedMessages: Message[],
	localMessages: Message[],
	localMessage: Message,
	localIndex?: number,
): Message | undefined {
	const exactMatch = persistedMessages.find((message) => message.id === localMessage.id);
	if (exactMatch) return exactMatch;

	if (!isOptimisticMessage(localMessage)) return undefined;

	// 流式阶段的 optimistic 消息和落库消息 id 不同，这里按同角色出现顺序兜底配对，
	// 避免 markdown/空白字符略有差异时把同一轮回复渲染成两条。
	const roleOccurrence = countMessagesByRole(localMessages, localMessage.role, localIndex);
	return findMessageByRoleOccurrence(persistedMessages, localMessage.role, roleOccurrence);
}

/**
 * 判断本地消息在 merge 后是否仍应保留。
 * 已能配对到落库消息则丢弃；乐观且内容次数多于落库侧则保留（尚未落库的那条）。
 */
function shouldKeepLocalMessage(
	persistedMessages: Message[],
	localMessages: Message[],
	localMessage: Message,
	localIndex: number,
): boolean {
	if (findPersistedMessageMatch(persistedMessages, localMessages, localMessage, localIndex)) {
		return false;
	}
	if (!isOptimisticMessage(localMessage)) return true;
	if (!messageMergeKey(localMessage)) return true;

	const localOccurrence = countMatchingMessages(localMessages, localMessage, localIndex);
	const persistedOccurrence = countMatchingMessages(persistedMessages, localMessage);
	return persistedOccurrence < localOccurrence;
}

/**
 * 消息排序比较：优先 sequence，否则按 timestamp。
 * 保证 merge 后时间线与后端顺序一致。
 */
function compareMessages(a: Message, b: Message): number {
	if (a.sequence !== undefined && b.sequence !== undefined) {
		return a.sequence - b.sequence;
	}
	return a.timestamp - b.timestamp;
}

/**
 * 合并落库附件与本地附件：只补 size/storageUri，不回填 blob/data。
 * 消息缩略图统一走 fileUploadId / 持久 URL（与 mapComposerAttachments 同策略）。
 */
export function mergeMessageAttachments(
	persistedAttachments: MessageAttachment[] | undefined,
	localAttachments: MessageAttachment[] | undefined,
): MessageAttachment[] | undefined {
	if (!persistedAttachments?.length) return persistedAttachments;
	if (!localAttachments?.length) return persistedAttachments;

	const localByUploadId = new Map(
		localAttachments.map((attachment) => [attachment.fileUploadId, attachment] as const),
	);

	return persistedAttachments.map((attachment) => {
		const localAttachment = localByUploadId.get(attachment.fileUploadId);
		if (!localAttachment) return attachment;
		return {
			...attachment,
			size: attachment.size || localAttachment.size,
			storageUri: attachment.storageUri || localAttachment.storageUri,
		};
	});
}

/**
 * 把落库消息与本地同轮消息对齐后，合并附件元数据（不回填临时预览 URL）。
 */
function reconcilePersistedMessagesWithLocal(
	persistedMessages: Message[],
	localMessages: Message[],
): Message[] {
	return persistedMessages.map((persistedMessage, persistedIndex) => {
		const roleOccurrence = countMessagesByRole(
			persistedMessages,
			persistedMessage.role,
			persistedIndex,
		);
		const localMatch =
			localMessages.find((localMessage) => localMessage.id === persistedMessage.id) ??
			localMessages.find((localMessage) => {
				if (localMessage.role !== persistedMessage.role) return false;
				if (messageMergeKey(localMessage) !== messageMergeKey(persistedMessage)) return false;
				return (
					countMatchingMessages(localMessages, localMessage) ===
					countMatchingMessages(persistedMessages, persistedMessage)
				);
			}) ??
			findMessageByRoleOccurrence(localMessages, persistedMessage.role, roleOccurrence);

		if (!localMatch?.attachments?.length || !persistedMessage.attachments?.length) {
			return persistedMessage;
		}

		return {
			...persistedMessage,
			attachments: mergeMessageAttachments(persistedMessage.attachments, localMatch.attachments),
		};
	});
}

/**
 * 合并 GetSessionMessages 落库结果与本地乐观消息。
 * 以落库为准，保留尚未落库的乐观条目，并按 sequence/timestamp 排序。
 */
export function mergeSessionMessages(
	persistedMessages: Message[],
	localMessages: Message[],
): Message[] {
	const reconciledPersistedMessages = reconcilePersistedMessagesWithLocal(
		persistedMessages,
		localMessages,
	);
	const merged = [...reconciledPersistedMessages];
	localMessages.forEach((localMessage, index) => {
		if (!shouldKeepLocalMessage(reconciledPersistedMessages, localMessages, localMessage, index)) {
			return;
		}
		if (merged.some((message) => message.id === localMessage.id)) return;
		merged.push(localMessage);
	});
	return merged.sort(compareMessages);
}

/** 从 ChatState 取出属于指定 session 的消息列表（按 messageIds 顺序）。 */
export function getSessionLocalMessages(state: ChatState, sessionId: string): Message[] {
	return state.messageIds
		.map((id) => state.messagesMap[id])
		.filter((message): message is Message => message?.conversationId === sessionId);
}

/** 判断消息是否处于「生成中」可视态（waiting / streaming）。 */
function isActiveStreamingMessage(message: Message | undefined): boolean {
	return message?.status === "waiting" || message?.status === "streaming";
}

/**
 * 切换 session 时仅保留目标会话消息，并同步流式状态，避免多任务并发时消息串台。
 */
export function retainLocalMessagesForSession(state: ChatState, sessionId: string): ChatState {
	const retainedIds: string[] = [];
	const retainedMap: Record<string, Message> = {};

	for (const id of state.messageIds) {
		const message = state.messagesMap[id];
		if (message?.conversationId === sessionId) {
			retainedIds.push(id);
			retainedMap[id] = message;
		}
	}

	const streamingMessageId =
		state.streamingMessageId && retainedMap[state.streamingMessageId]
			? state.streamingMessageId
			: (retainedIds.findLast((id) => isActiveStreamingMessage(retainedMap[id])) ?? null);
	const streamingMessage = streamingMessageId ? retainedMap[streamingMessageId] : undefined;
	const isGenerating = isActiveStreamingMessage(streamingMessage);

	return {
		...state,
		messagesMap: retainedMap,
		messageIds: retainedIds,
		streamingMessageId,
		isGenerating,
		streamCancelRef: isGenerating ? state.streamCancelRef : null,
	};
}

/**
 * 本地时间线上的消息是否全部属于指定 session。
 * 用于进页/切会话时判断是否需要清屏或重新加载。
 */
export function allLocalMessagesBelongToSession(state: ChatState, sessionId: string): boolean {
	if (state.messageIds.length === 0) return true;
	return state.messageIds.every((id) => state.messagesMap[id]?.conversationId === sessionId);
}

/**
 * 从未知 payload 解析 `work.title.updated` 结构；字段不合法时返回 null。
 * fallbackSessionId 用于 payload 缺 session_id 时补全。
 */
export function parseWorkTitleUpdatedRecord(
	payload: unknown,
	fallbackSessionId?: string,
): BackendWorkTitleUpdatedPayload | null {
	if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
		return null;
	}
	const record = payload as Record<string, unknown>;
	if (typeof record.project_id !== "string" || typeof record.project_name !== "string") {
		return null;
	}
	return {
		project_id: record.project_id,
		project_name: record.project_name,
		task_id: typeof record.task_id === "string" ? record.task_id : undefined,
		task_title: typeof record.task_title === "string" ? record.task_title : undefined,
		session_id:
			typeof record.session_id === "string" ? record.session_id : (fallbackSessionId ?? ""),
		session_title: typeof record.session_title === "string" ? record.session_title : undefined,
	};
}

/** 从 SessionEvents 的 SSEMessageEvent 解析标题更新 payload。 */
export function parseWorkTitleUpdatedPayload(
	data: SSEMessageEvent,
): BackendWorkTitleUpdatedPayload | null {
	return parseWorkTitleUpdatedRecord(data.payload, data.session_id);
}

/**
 * 解析 GlobalEvents 原始 JSON 字符串；失败返回 null。
 * fallbackType 用于传输层已给出 event.type 时覆盖/补齐。
 */
export function parseGlobalEvent(raw: string, fallbackType?: string): BackendGlobalEvent | null {
	try {
		const parsed = JSON.parse(raw) as BackendGlobalEvent;
		return {
			...parsed,
			type: fallbackType ?? parsed.type,
		};
	} catch {
		return null;
	}
}

/** 统一读取 GlobalEvents 的 data/payload 字段为消息 payload。 */
export function getGlobalMessagePayload(event: BackendGlobalEvent): BackendGlobalMessagePayload {
	const payload = event.data ?? event.payload;
	return isRecord(payload) ? (payload as BackendGlobalMessagePayload) : {};
}

/**
 * 由 GlobalEvents `message.created`(human) 构造前端用户 Message。
 * 无 session 或正文与附件皆空时返回 undefined。
 */
export function createGlobalUserMessageFromEvent(
	event: BackendGlobalEvent,
	payload: BackendGlobalMessagePayload,
): Message | undefined {
	const sessionId = event.session_id;
	const content = payload.content ?? "";
	const createdAt = parseOptionalTimestamp(payload.created_at) ?? event.timestamp;
	const attachments = mapGlobalMessageAttachments(payload.attachments, createdAt);
	const metadata = buildMessageMetadata(payload.metadata);
	if (!sessionId || (!content.trim() && !attachments?.length)) return undefined;

	const messageKey = payload.message_id ?? payload.id ?? payload.sequence;
	const messageId =
		messageKey !== undefined
			? String(messageKey)
			: `global-user-${sessionId}-${event.timestamp || createdAt || Date.now()}`;

	return {
		id: messageId,
		conversationId: sessionId,
		role: "user",
		content,
		timestamp: event.timestamp || createdAt || Date.now(),
		sequence: payload.sequence,
		status: "completed",
		author: {
			id: payload.sender_uin !== undefined ? String(payload.sender_uin) : "user",
			name: payload.sender_name || "用户",
			type: "user",
		},
		attachments,
		metadata,
	};
}

/**
 * 把 GlobalEvents 用户消息 id 插入时间线：优先插在本轮 waiting/streaming assistant 之前，
 * 保证「问题在上、回复在下」；已存在则原样返回。
 */
export function insertGlobalUserMessageId(
	messageIds: string[],
	messagesMap: Record<string, Message>,
	incoming: Message,
	activeStreamingMessageId?: string | null,
): string[] {
	const existingIndex = messageIds.indexOf(incoming.id);
	if (existingIndex >= 0) return messageIds;

	const activeStreamingAssistantIndex = activeStreamingMessageId
		? messageIds.indexOf(activeStreamingMessageId)
		: -1;
	if (activeStreamingAssistantIndex >= 0) {
		const activeStreamingMessage = messagesMap[activeStreamingMessageId ?? ""];
		if (
			activeStreamingMessage?.conversationId === incoming.conversationId &&
			activeStreamingMessage.role === "assistant" &&
			(activeStreamingMessage.status === "waiting" || activeStreamingMessage.status === "streaming")
		) {
			// 只锚定本轮正在生成的 assistant，避免历史回放消息被标记为 streaming 后抢走插入位置。
			return [
				...messageIds.slice(0, activeStreamingAssistantIndex),
				incoming.id,
				...messageIds.slice(activeStreamingAssistantIndex),
			];
		}
	}

	let waitingAssistantIndex = -1;
	for (let index = messageIds.length - 1; index >= 0; index -= 1) {
		const message = messagesMap[messageIds[index] ?? ""];
		if (isTaskRoomAssistantPlaceholder(message, incoming.conversationId)) {
			waitingAssistantIndex = index;
			break;
		}
	}
	if (waitingAssistantIndex >= 0) {
		// 兜底使用最新任务等待占位，确保用户问题仍显示在本轮回复前。
		return [
			...messageIds.slice(0, waitingAssistantIndex),
			incoming.id,
			...messageIds.slice(waitingAssistantIndex),
		];
	}

	const streamingAssistantIndex = messageIds.findIndex((id) => {
		const message = messagesMap[id];
		return (
			message?.conversationId === incoming.conversationId &&
			message.role === "assistant" &&
			message.status === "waiting"
		);
	});
	if (streamingAssistantIndex >= 0) {
		// 兼容非任务占位的等待态 assistant，仍然只允许 waiting 作为插入锚点。
		return [
			...messageIds.slice(0, streamingAssistantIndex),
			incoming.id,
			...messageIds.slice(streamingAssistantIndex),
		];
	}

	return [...messageIds, incoming.id];
}
