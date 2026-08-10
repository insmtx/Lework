/**
 * SessionEvents / 历史 chunks → Message 的纯函数层。
 *
 * 可以做：把后端事件形状映射进气泡字段（正文过程、工具、todo、产物、审批/问答、run 终态）；
 * 以及 `mapBackendMessage` 等后端 DTO → 前端 Message 映射。
 * 不可以做：发 HTTP、开/关 SSE、读写 Zustand、导航或改 layout。
 */

import type {
	BackendApprovalDecisionPayload,
	BackendApprovalRequestPayload,
	BackendMessage,
	BackendMessageAttachment,
	BackendMessageChunk,
	BackendQuestionAnswerPayload,
	BackendQuestionRequestPayload,
	BackendRuntimeTodoItem,
	BackendSessionArtifactPayload,
	BackendSessionEventPayload,
	BackendToolCall,
	SSEMessageEvent,
} from "../api/types";
import type {
	ApprovalAction,
	ApprovalRequest,
	Message,
	MessageArtifact,
	MessageAttachment,
	MessageMetadata,
	MessageProcessStep,
	MessageRole,
	MessageUsage,
	QuestionItem,
	QuestionRequest,
	RuntimeTodoItem,
	TodoStatus,
	ToolCall,
	ToolCallStatus,
} from "../types/chat";
import { formatFileSize, parseOptionalTimestamp } from "../utils/format";
import {
	buildMessageMetadata,
	enrichAssistantMessageMetrics,
	latencyFromRunCompletedTimes,
} from "../utils/messageMetrics";

/**
 * 将后端 SessionMessage 转为前端 Message。
 * 会回放 chunks 还原 tool/thinking 过程，并补齐 artifacts、attachments、用量指标。
 */
export function mapBackendMessage(msg: BackendMessage): Message {
	const message: Message = {
		id: String(msg.id),
		conversationId: msg.session_id ?? msg.conversation_id ?? "",
		role: msg.role as MessageRole,
		content: msg.content ?? "",
		timestamp: msg.timestamp ?? new Date(msg.created_at).getTime(),
		sequence: msg.sequence,
		runId: msg.run_id,
		author:
			msg.role === "user" && msg.sender_uin !== undefined
				? {
						id: String(msg.sender_uin),
						name: msg.sender_name || "用户",
						type: "user",
					}
				: msg.role === "assistant" && msg.sender_name
					? {
							id: msg.run_id ?? String(msg.id),
							name: msg.sender_name,
							type: "assistant",
						}
					: undefined,
		metadata: buildMessageMetadata(msg.metadata),
		usage: mapUsage(msg.usage),
	};

	let mapped = applySessionEventsToMessage(message, msg.chunks, {
		appendContent: !message.content,
		finalContent: message.content,
		// 中文注释：历史消息的 chunks 只用于还原执行过程，不能重新把已落库回复标成生成中。
		markStreaming: false,
	});
	if (
		mapped.role === "assistant" &&
		mapped.todos?.length &&
		mapped.status !== "failed" &&
		mapped.status !== "streaming" &&
		mapped.status !== "waiting" &&
		mapped.status !== "sending" &&
		(mapped.status === "completed" ||
			Boolean(mapped.content?.trim() || mapped.processSteps?.length))
	) {
		mapped = {
			...mapped,
			todos: completeTodos(mapped.todos),
		};
	}
	if (msg.artifacts?.length) {
		const artifacts = msg.artifacts
			.map(mapArtifactPayload)
			.filter((artifact): artifact is MessageArtifact => artifact !== undefined);
		if (artifacts.length) {
			mapped = {
				...mapped,
				artifacts: mergeArtifacts(mapped.artifacts, artifacts),
			};
		}
	}
	if (msg.attachments?.length) {
		const messageCreatedAt = parseOptionalTimestamp(msg.created_at) ?? msg.timestamp;
		const attachments = msg.attachments
			.map((attachment) => mapBackendAttachment(attachment, messageCreatedAt))
			.filter((attachment): attachment is MessageAttachment => attachment !== undefined);
		if (attachments.length) {
			mapped = { ...mapped, attachments };
		}
	}
	return enrichAssistantMessageMetrics(mapped);
}

/**
 * 将后端附件 DTO 转为前端 MessageAttachment。
 * 缺少 file_upload_id 时返回 undefined（无法作为可下载/预览附件）。
 */
export function mapBackendAttachment(
	attachment: BackendMessageAttachment,
	messageCreatedAt?: number,
): MessageAttachment | undefined {
	const fileUploadId = attachment.file_upload_id?.trim();
	if (!fileUploadId) return undefined;

	return {
		id: fileUploadId,
		fileUploadId,
		name: attachment.name?.trim() || fileUploadId,
		mimeType: attachment.mime_type?.trim() || "application/octet-stream",
		size: attachment.size ?? 0,
		relativePath: attachment.relative_path?.trim() || undefined,
		createdAt: messageCreatedAt,
		url: attachment.PublicURL?.trim() || attachment.public_url?.trim() || undefined,
	};
}

/** 批量映射后端 tool_calls 列表为前端 ToolCall[]。 */
function mapToolCalls(tcList?: BackendToolCall[]): ToolCall[] | undefined {
	if (!tcList) return undefined;
	return tcList.map((tc) => ({
		id: tc.id,
		name: tc.name,
		arguments: tc.arguments ?? {},
		status: normalizeToolCallStatus(tc.status),
		result: tc.result,
		duration: tc.duration,
	}));
}

type NormalizedSessionEvent = Exclude<BackendMessageChunk, string> | SSEMessageEvent;
type SessionEventLike = BackendMessageChunk | SSEMessageEvent;

/**
 * 从 run_id（形如 req_<userMessageId>）解析出被回复的用户消息 id。
 * 用于 assistant 气泡展示「回复了哪条问题」。
 */
function getReplyTargetMessageId(runId?: string): string | undefined {
	const match = runId?.match(/^req_(.+)$/);
	return match?.[1]?.trim() || undefined;
}

/**
 * 由一条用户消息构造 replyTo 摘要（展示用，非协议字段）。
 * 非 user 或正文与附件皆空时返回 undefined。
 */
export function buildReplyToFromMessage(message?: Message): Message["replyTo"] | undefined {
	if (message?.role !== "user") return undefined;
	const content = (message.metadata?.displayContent ?? message.content).trim();
	if (!content && !message.attachments?.length) return undefined;
	return {
		messageId: message.id,
		authorName: message.author?.name,
		content,
	};
}

/**
 * 从当前 streaming 消息上读取 runId，供 CancelSessionRun 使用。
 * 无流式消息或尚未绑 runId 时返回 undefined。
 */
export function resolveActiveRunIdForCancel(
	state: Pick<
		{ streamingMessageId: string | null; messagesMap: Record<string, Message> },
		"streamingMessageId" | "messagesMap"
	>,
) {
	const streamingMessage = state.streamingMessageId
		? state.messagesMap[state.streamingMessageId]
		: undefined;
	// 中文注释：run_id 由 GlobalEvents 的 message.created 写入流式 assistant 消息，取消时一并传给后端。
	return streamingMessage?.runId?.trim() || undefined;
}

/**
 * 根据 runId 在 messagesMap 中定位用户消息，并生成 assistant 的 replyTo。
 * sessionId 用于防止跨会话误绑。
 */
export function resolveReplyToFromRunId(
	runId: string | undefined,
	messagesMap: Record<string, Message>,
	sessionId?: string,
): Message["replyTo"] | undefined {
	const replyTargetMessageId = getReplyTargetMessageId(runId);
	if (!replyTargetMessageId) return undefined;
	const target = messagesMap[replyTargetMessageId];
	if (!target || (sessionId && target.conversationId !== sessionId)) return undefined;
	return buildReplyToFromMessage(target);
}

/**
 * 批量为尚无 replyTo 的 assistant 消息补上回复目标。
 * 通常在 GetSessionMessages 映射后调用，方便气泡展示引用。
 */
export function attachAssistantReplyTargets(messages: Message[]): Message[] {
	const messagesMap = Object.fromEntries(messages.map((message) => [message.id, message]));
	return messages.map((message) => {
		if (message.role !== "assistant" || message.replyTo) return message;
		const replyTo = resolveReplyToFromRunId(message.runId, messagesMap, message.conversationId);
		if (!replyTo) return message;
		return { ...message, replyTo };
	});
}

/** 将后端 snake_case token 用量转为前端 MessageUsage。 */
export function mapUsage(usage?: {
	input_tokens?: number;
	output_tokens?: number;
	total_tokens?: number;
}): MessageUsage | undefined {
	if (!usage) return undefined;
	if (
		usage.input_tokens === undefined &&
		usage.output_tokens === undefined &&
		usage.total_tokens === undefined
	) {
		return undefined;
	}
	const inputTokens = usage.input_tokens;
	const outputTokens = usage.output_tokens;
	const summed =
		inputTokens !== undefined || outputTokens !== undefined
			? (inputTokens ?? 0) + (outputTokens ?? 0)
			: undefined;
	// 中文注释：优先用后端 total_tokens；缺失或为 0 时用 input+output 回算，便于 footer 展示。
	const totalTokens =
		usage.total_tokens !== undefined && usage.total_tokens > 0
			? usage.total_tokens
			: (summed ?? usage.total_tokens);
	return {
		inputTokens,
		outputTokens,
		totalTokens,
	};
}

/** 归一化后端工具状态字符串为前端 ToolCallStatus 枚举。 */
function normalizeToolCallStatus(status?: string): ToolCallStatus {
	switch (status) {
		case "success":
		case "completed":
			return "success";
		case "error":
		case "failed":
			return "error";
		case "running":
		case "in_progress":
			return "running";
		default:
			return "pending";
	}
}

/** 归一化后端 todo 状态字符串为前端 TodoStatus 枚举。 */
function normalizeTodoStatus(status?: string): TodoStatus {
	switch (status) {
		case "in_progress":
			return "in_progress";
		case "completed":
			return "completed";
		case "cancelled":
			return "cancelled";
		default:
			return "pending";
	}
}

/**
 * run 完成时把未完成的 todo 一律标为 completed。
 * 避免终态气泡上仍显示进行中的待办。
 */
export function completeTodos(todos: RuntimeTodoItem[] | undefined): RuntimeTodoItem[] | undefined {
	if (!todos?.length || todos.every((todo) => todo.status === "completed")) {
		return todos;
	}
	return todos.map((todo) =>
		todo.status === "completed" ? todo : { ...todo, status: "completed" },
	);
}

/** 类型守卫：判断值是否为非 null 的普通对象。 */
export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

/**
 * 将历史 chunk（可能是 JSON 字符串）或 SSE 事件规范为带 type 的对象。
 * 无法解析时返回 undefined，调用方应跳过该事件。
 */
function normalizeSessionEvent(event: SessionEventLike): NormalizedSessionEvent | undefined {
	if (typeof event === "string") {
		try {
			const parsed = JSON.parse(event) as unknown;
			if (isRecord(parsed) && typeof parsed.type === "string") {
				return parsed as NormalizedSessionEvent;
			}
		} catch {
			return undefined;
		}
		return undefined;
	}

	if (typeof event.type !== "string") return undefined;
	return event as NormalizedSessionEvent;
}

/**
 * 从规范化事件中取出 payload。
 * 兼容 payload 为数组（旧 todo 快照）或字段直接挂在事件根上的形状。
 */
function getEventPayload(event: NormalizedSessionEvent): BackendSessionEventPayload {
	if (Array.isArray(event.payload)) {
		return { todos: event.payload };
	}
	if (isRecord(event.payload)) {
		return event.payload as BackendSessionEventPayload;
	}
	return event as BackendSessionEventPayload;
}

/** 从 payload / 事件根字段中提取文本内容（content / message / chunk）。 */
function getEventContent(
	event: NormalizedSessionEvent,
	payload: BackendSessionEventPayload,
): string {
	return (
		payload.content ??
		payload.message ??
		("content" in event ? event.content : undefined) ??
		("chunk" in event ? event.chunk : undefined) ??
		""
	);
}

/** 从 run.completed 的 payload 中提取最终回复正文。 */
function getRunResultMessage(payload: BackendSessionEventPayload): string | undefined {
	if (typeof payload.message === "string" && payload.message.trim()) {
		return payload.message;
	}
	if (!payload.result || typeof payload.result !== "object") return undefined;
	const value = payload.result as { message?: unknown };
	return typeof value.message === "string" ? value.message : undefined;
}

/** 从 run.failed 的 payload 中提取错误文案（优先 error，其次 result.message）。 */
function getRunFailedMessage(payload: BackendSessionEventPayload): string | undefined {
	if (typeof payload.error === "string" && payload.error.trim()) {
		return payload.error;
	}
	return getRunResultMessage(payload);
}

/**
 * 按阶段关键词/空行切分 thinking 文本，便于过程区展示多个步骤。
 * 仅影响展示切分，不改变原始流内容。
 */
function splitThinkingStepContent(content: string): string[] {
	const normalized = content.replace(/\r\n/g, "\n");
	const withStageBoundaries = normalized.replace(
		/\n(?=(?:\*\*)?(?:下一步|接下来|现在|然后|最后|首先)[：:]?|#{1,6}\s)/g,
		"\n\n",
	);

	return withStageBoundaries
		.split(/\n{2,}/)
		.map((segment) => segment.trim())
		.filter(Boolean);
}

/** 将切分后的 thinking 片段转为 processSteps（type=thinking）。 */
function createThinkingStepsFromContent(content: string, startIndex: number): MessageProcessStep[] {
	const thinkingSegments = splitThinkingStepContent(content);
	return thinkingSegments.map((segment, index) => ({
		id: `thinking-${startIndex + index + 1}`,
		type: "thinking" as const,
		content: segment,
	}));
}

/**
 * 用完整 thinking 文本重建末尾 thinking 步骤（先去掉原末尾 thinking 再按片段重建）。
 * 用于 delta 追加后重新分段，避免步骤粘成一团。
 */
function rebuildTrailingThinkingSteps(
	steps: MessageProcessStep[] | undefined,
	content: string,
): MessageProcessStep[] {
	const stableSteps = [...(steps ?? [])];
	return [...stableSteps, ...createThinkingStepsFromContent(content, stableSteps.length)];
}

/**
 * 向 processSteps 追加一段 thinking delta。
 * 若末尾已是 thinking，则合并后再按规则重新切分。
 */
function appendProcessThinkingStep(
	steps: MessageProcessStep[] | undefined,
	delta: string,
): MessageProcessStep[] {
	if (!delta) return steps ?? [];

	const next = [...(steps ?? [])];
	const lastStep = next.at(-1);
	if (lastStep?.type === "thinking") {
		return rebuildTrailingThinkingSteps(next.slice(0, -1), lastStep.content + delta);
	}

	return rebuildTrailingThinkingSteps(next, delta);
}

/**
 * 向 processSteps 追加一条 tool_call 步骤；同一 toolCallId 不重复插入。
 */
function appendProcessToolCallStep(
	steps: MessageProcessStep[] | undefined,
	toolCallId: string,
): MessageProcessStep[] {
	if (!toolCallId) return steps ?? [];
	if (steps?.some((step) => step.type === "tool_call" && step.toolCallId === toolCallId)) {
		return steps;
	}

	return [
		...(steps ?? []),
		{
			id: `tool-call-${toolCallId}`,
			type: "tool_call",
			toolCallId,
		},
	];
}

/** 应用 SessionEvents 时的行为开关。 */
type ApplySessionEventOptions = {
	appendContent: boolean;
	finalContent?: string;
	markStreaming?: boolean;
};

/**
 * 在允许标 streaming 时把消息标为 streaming 并清掉 statusText。
 * 历史回放（markStreaming=false）时原样返回，避免把已完成消息重新标成生成中。
 */
function applyStreamingState<T extends Message>(message: T, options: ApplySessionEventOptions): T {
	if (options.markStreaming === false) return message;
	return {
		...message,
		status: "streaming",
		statusText: undefined,
	};
}

/**
 * 判断 message.delta 是否应写入 processSteps。
 * 历史消息最终正文已在 content 时，属于最终回答的 delta 不再进过程区。
 */
function shouldAppendMessageDeltaToProcess(
	content: string,
	options: ApplySessionEventOptions,
): boolean {
	const trimmedContent = content.trim();
	if (!trimmedContent) return false;

	const finalContent = options.finalContent?.trim();
	if (!finalContent) return true;

	// 历史消息里最终回答已在 content 字段，属于最终回答的 delta 不再放入执行过程。
	return !finalContent.includes(trimmedContent);
}

/**
 * 终态时剔除 processSteps 中已出现在最终 content 里的 thinking，避免过程区与正文重复。
 */
function pruneFinalContentProcessSteps(
	steps: MessageProcessStep[] | undefined,
	finalContent: string | undefined,
): MessageProcessStep[] | undefined {
	const trimmedFinalContent = finalContent?.trim();
	if (!steps?.length || !trimmedFinalContent) return steps;

	const nextSteps = steps.filter((step) => {
		if (step.type !== "thinking") return true;
		const content = step.content.trim();
		return !!content && !trimmedFinalContent.includes(content);
	});

	return nextSteps.length ? nextSteps : undefined;
}

/** 从 run 终态 payload 提取/合并 metadata（含模型与流式耗时）。 */
function metadataFromPayload(payload: BackendSessionEventPayload): MessageMetadata | undefined {
	const usage = mapUsage(payload.usage ?? payload);
	const streamLatency = latencyFromRunCompletedTimes(payload.started_at, payload.completed_at);
	return buildMessageMetadata(
		{
			...payload.metadata,
			model: payload.metadata?.model ?? payload.model,
			latency: payload.metadata?.latency ?? streamLatency,
		},
		usage,
	);
}

/** 将一组 tool call 更新依次 upsert 到现有列表。 */
function mergeToolCalls(current: ToolCall[] | undefined, updates: ToolCall[]): ToolCall[] {
	return updates.reduce((acc, update) => upsertToolCall(acc, update), current ?? []);
}

/** 校验 unknown 是否为 todo 对象数组；否则返回 undefined。 */
function getTodoItemsFromValue(value: unknown): BackendRuntimeTodoItem[] | undefined {
	if (!Array.isArray(value)) return undefined;
	if (!value.every(isRecord)) return undefined;
	return value as BackendRuntimeTodoItem[];
}

/** 将后端 todo 项映射为前端 RuntimeTodoItem（补默认 id/title/status）。 */
function mapTodoItems(items: BackendRuntimeTodoItem[]): RuntimeTodoItem[] {
	return items.map((item, index) => ({
		id: item.id?.trim() || `todo-${index + 1}`,
		title: item.title?.trim() || `待办 ${index + 1}`,
		status: normalizeTodoStatus(item.status),
		priority: item.priority,
	}));
}

/**
 * 将 artifact.declared / 历史 artifacts 项映射为前端 MessageArtifact。
 * 缺少 artifact_id 时返回 undefined。
 */
export function mapArtifactPayload(
	payload: BackendSessionArtifactPayload,
): MessageArtifact | undefined {
	const artifactID = payload.artifact_id?.trim();
	if (!artifactID) return undefined;

	const artifactType = payload.artifact_type?.trim() || "file";
	const mimeType = payload.mime_type?.trim();
	const filename = payload.filename?.trim();
	const title = payload.title?.trim() || filename || artifactID;
	const type =
		mimeType?.startsWith("image/") || artifactType === "image"
			? "image"
			: artifactType === "spreadsheet"
				? "spreadsheet"
				: "document";

	return {
		id: artifactID,
		name: filename || title,
		title,
		description: payload.description?.trim() || undefined,
		type,
		artifactType,
		mimeType,
		size: formatFileSize(payload.file_size ?? 0),
		updatedAt: parseOptionalTimestamp(payload.created_at),
		downloadUrl: "",
		storageUri: payload.storage_uri?.trim() || undefined,
		sha256: payload.sha256,
		versionNo: payload.version_no,
	};
}

/** 按 artifact id 合并列表：已存在则浅合并字段，否则追加。 */
export function mergeArtifacts(
	current: MessageArtifact[] | undefined,
	updates: MessageArtifact[],
): MessageArtifact[] {
	const next = [...(current ?? [])];
	for (const update of updates) {
		const index = next.findIndex((artifact) => artifact.id === update.id);
		if (index === -1) {
			next.push(update);
			continue;
		}
		next[index] = { ...next[index], ...update };
	}
	return next;
}

/** 将后端审批动作字符串规范为 approve/deny/always；非法值返回 undefined。 */
function normalizeApprovalAction(action?: string): ApprovalAction | undefined {
	switch (action) {
		case "approve":
		case "deny":
		case "always":
			return action;
		default:
			return undefined;
	}
}

/** 由审批动作推导前端审批状态（pending/approved/denied/always）。 */
export function getApprovalStatus(action?: string): ApprovalRequest["status"] {
	switch (action) {
		case "approve":
			return "approved";
		case "deny":
			return "denied";
		case "always":
			return "always";
		default:
			return "pending";
	}
}

/** 将 approval.requested payload 映射为前端 ApprovalRequest。 */
function mapApprovalRequestPayload(
	payload: BackendApprovalRequestPayload,
	assistantId?: string,
): ApprovalRequest | undefined {
	const requestId = payload.request_id?.trim();
	if (!requestId) return undefined;

	return {
		requestId,
		toolName: payload.tool_name?.trim() || "Tool",
		toolCallId: payload.tool_call_id?.trim() || undefined,
		description: payload.description?.trim() || "需要审批后继续执行",
		arguments: payload.arguments,
		metadata: payload.metadata,
		status: "pending",
		assistantId,
	};
}

/** 将 approval.resolved payload 映射为决策摘要字段。 */
function mapApprovalDecisionPayload(
	payload: BackendApprovalDecisionPayload,
): Pick<ApprovalRequest, "requestId" | "status" | "action" | "reason"> | undefined {
	const requestId = payload.request_id?.trim();
	if (!requestId) return undefined;
	const action = normalizeApprovalAction(payload.action);

	return {
		requestId,
		status: getApprovalStatus(action),
		action,
		reason: payload.reason?.trim() || undefined,
	};
}

/**
 * 合并新的审批请求：同 requestId 时保留已决状态，避免重复 requested 冲掉用户已选结果。
 */
function mergeApprovalRequest(
	current: ApprovalRequest[] | undefined,
	update: ApprovalRequest,
): ApprovalRequest[] {
	const list = current ?? [];
	const index = list.findIndex((approval) => approval.requestId === update.requestId);
	if (index === -1) return [...list, update];

	const existing = list[index];
	if (!existing) return [...list, update];

	const next = [...list];
	next[index] = {
		...existing,
		...update,
		status: existing.status === "pending" ? update.status : existing.status,
		action: existing.action ?? update.action,
		reason: existing.reason ?? update.reason,
		error: existing.status === "error" ? existing.error : update.error,
	};
	return next;
}

/** 将审批决策写回列表；找不到原请求时补一条最小记录。 */
function mergeApprovalDecision(
	current: ApprovalRequest[] | undefined,
	decision: Pick<ApprovalRequest, "requestId" | "status" | "action" | "reason">,
): ApprovalRequest[] {
	const list = current ?? [];
	const index = list.findIndex((approval) => approval.requestId === decision.requestId);
	if (index === -1) {
		return [
			...list,
			{
				requestId: decision.requestId,
				toolName: "Tool",
				description: "审批已处理",
				status: decision.status,
				action: decision.action,
				reason: decision.reason,
			},
		];
	}

	const existing = list[index];
	if (!existing) return list;

	const next = [...list];
	next[index] = {
		...existing,
		status: decision.status,
		action: decision.action ?? existing.action,
		reason: decision.reason ?? existing.reason,
		error: undefined,
	};
	return next;
}

/** 从 SessionEvent payload 中取出审批请求（兼容嵌套与扁平两种形状）。 */
function getApprovalRequestPayload(
	payload: BackendSessionEventPayload,
): BackendApprovalRequestPayload | undefined {
	if (payload.approval_request) return payload.approval_request;
	if (payload.request_id || payload.tool_name) return payload;
	return undefined;
}

/** 从 SessionEvent payload 中取出审批决策（兼容嵌套与扁平两种形状）。 */
function getApprovalDecisionPayload(
	payload: BackendSessionEventPayload,
): BackendApprovalDecisionPayload | undefined {
	if (payload.approval_decision) return payload.approval_decision;
	if (payload.request_id || payload.action) return payload;
	return undefined;
}

/** 从 SessionEvent payload 中取出问答请求。 */
function getQuestionRequestPayload(
	payload: BackendSessionEventPayload,
): BackendQuestionRequestPayload | undefined {
	if (payload.question_request) return payload.question_request;
	if (payload.request_id && payload.questions) return payload as BackendQuestionRequestPayload;
	return undefined;
}

/** 从 SessionEvent payload 中取出问答答案。 */
function getQuestionAnswerPayload(
	payload: BackendSessionEventPayload,
): BackendQuestionAnswerPayload | undefined {
	if (payload.question_answer) return payload.question_answer;
	if (payload.request_id && payload.answers) return payload as BackendQuestionAnswerPayload;
	return undefined;
}

/** 将 question.asked payload 映射为前端 QuestionRequest。 */
function mapQuestionRequestPayload(
	payload: BackendQuestionRequestPayload,
	assistantId?: string,
): QuestionRequest | undefined {
	const requestId = payload.request_id?.trim();
	if (!requestId) return undefined;

	const questions: QuestionItem[] = (payload.questions ?? []).map((q) => ({
		question: q.question,
		header: q.header,
		options: (q.options ?? []).map((o) => ({
			label: o.label,
			description: o.description,
		})),
		multiple: q.multiple ?? false,
		custom: q.custom ?? false,
	}));

	return {
		requestId,
		questions,
		assistantId,
		toolCallId: payload.tool_call_id?.trim() || undefined,
		messageId: payload.message_id?.trim() || undefined,
		interactionType: payload.interaction_type?.trim() || undefined,
		metadata: payload.metadata,
		status: "pending",
	};
}

/** 将 question.answered payload 映射为答案摘要字段。 */
function mapQuestionAnswerPayload(
	payload: BackendQuestionAnswerPayload,
): Pick<QuestionRequest, "requestId" | "status" | "answers"> | undefined {
	const requestId = payload.request_id?.trim();
	if (!requestId) return undefined;

	return {
		requestId,
		status: "answered",
		answers: payload.answers ?? [],
	};
}

/** 合并问答请求；同 requestId 且已非 pending 时不降级状态。 */
function mergeQuestionRequest(
	current: QuestionRequest[] | undefined,
	update: QuestionRequest,
): QuestionRequest[] {
	const list = current ?? [];
	const index = list.findIndex((q) => q.requestId === update.requestId);
	if (index === -1) return [...list, update];

	const next = [...list];
	next[index] = {
		...next[index],
		...update,
		status:
			(next[index]?.status ?? "pending") === "pending"
				? update.status
				: (next[index]?.status ?? "pending"),
	};
	return next;
}

/** 将用户答案写回 questions 列表；找不到原请求时补最小记录。 */
function mergeQuestionAnswer(
	current: QuestionRequest[] | undefined,
	answer: Pick<QuestionRequest, "requestId" | "status" | "answers">,
): QuestionRequest[] {
	const list = current ?? [];
	const index = list.findIndex((q) => q.requestId === answer.requestId);
	if (index === -1) {
		return [
			...list,
			{
				requestId: answer.requestId,
				questions: [],
				status: answer.status,
				answers: answer.answers,
			},
		];
	}

	const next = [...list];
	const existing = next[index];
	if (!existing) return list;

	next[index] = {
		...existing,
		status: answer.status,
		answers: answer.answers ?? existing.answers,
		error: undefined,
	};
	return next;
}

/** 从事件的多种可能位置提取并映射 todo 列表。 */
function getTodoItems(
	event: NormalizedSessionEvent,
	payload: BackendSessionEventPayload,
): RuntimeTodoItem[] | undefined {
	const payloadTodos = getTodoItemsFromValue(payload.todos);
	if (payloadTodos) return mapTodoItems(payloadTodos);

	if ("todos" in event) {
		const eventTodos = getTodoItemsFromValue(event.todos);
		if (eventTodos) return mapTodoItems(eventTodos);
	}

	const rawPayloadTodos = getTodoItemsFromValue(event.payload);
	if (rawPayloadTodos) return mapTodoItems(rawPayloadTodos);

	return undefined;
}

/** 按 tool call id 插入或浅合并更新；arguments 做字段级合并。 */
function upsertToolCall(current: ToolCall[] | undefined, update: ToolCall): ToolCall[] {
	const list = current ?? [];
	const index = list.findIndex((tc) => tc.id === update.id);
	if (index === -1) return [...list, update];

	const existing = list[index];
	if (!existing) return [...list, update];

	const next = [...list];
	next[index] = {
		...existing,
		...update,
		name: update.name || existing.name,
		arguments: {
			...existing.arguments,
			...update.arguments,
		},
		result: update.result ?? existing.result,
		duration: update.duration ?? existing.duration,
	};
	return next;
}

/**
 * 将 tool_call.* 事件映射为单个 ToolCall（含状态推断）。
 * 缺少 tool_call_id/id 时返回 undefined。
 */
function mapToolCallEvent(
	eventType: string,
	payload: BackendSessionEventPayload,
): ToolCall | undefined {
	const id = payload.tool_call_id ?? payload.id;
	if (!id) return undefined;

	const status =
		eventType === "tool_call.result" || eventType === "tool_call.completed"
			? normalizeToolCallStatus(payload.status ?? (payload.is_error ? "error" : "success"))
			: eventType === "tool_call.failed"
				? "error"
				: "running";

	return {
		id,
		name: payload.name ?? id,
		arguments: payload.arguments ?? {},
		status,
		result: payload.result ?? payload.error,
		duration: payload.duration ?? payload.elapsed_ms,
	};
}

/**
 * 将单条 SessionEvents 事件应用到 assistant Message（纯函数）。
 * 负责正文过程、工具、todo、产物、审批/问答与 run 终态；不触碰 store / 网络。
 */
export function applySessionEventToMessage(
	message: Message,
	event: SessionEventLike,
	eventType: string | undefined,
	options: ApplySessionEventOptions,
): Message {
	const normalizedEvent = normalizeSessionEvent(event);
	if (!normalizedEvent) return message;

	const normalizedEventType = eventType ?? normalizedEvent.type;
	const payload = getEventPayload(normalizedEvent);

	if (
		payload.tool_calls?.length ||
		("tool_calls" in normalizedEvent && normalizedEvent.tool_calls?.length)
	) {
		const toolCalls = mapToolCalls(
			payload.tool_calls ??
				("tool_calls" in normalizedEvent ? normalizedEvent.tool_calls : undefined),
		);
		if (toolCalls?.length) {
			return {
				...message,
				toolCalls: mergeToolCalls(message.toolCalls, toolCalls),
			};
		}
	}

	switch (normalizedEventType) {
		case "todo.snapshot":
		case "todo.updated": {
			const todos = getTodoItems(normalizedEvent, payload);
			if (!todos) return message;
			return { ...message, todos };
		}
		case "artifact.declared": {
			const artifact = mapArtifactPayload(payload);
			if (!artifact) return message;
			return {
				...message,
				artifacts: mergeArtifacts(message.artifacts, [artifact]),
			};
		}
		case "approval.requested": {
			const approvalPayload = getApprovalRequestPayload(payload);
			const approval = approvalPayload
				? mapApprovalRequestPayload(
						approvalPayload,
						"assistant_id" in normalizedEvent ? normalizedEvent.assistant_id : undefined,
					)
				: undefined;
			if (!approval) return message;
			return {
				...message,
				approvals: mergeApprovalRequest(message.approvals, approval),
			};
		}
		case "approval.resolved": {
			const decisionPayload = getApprovalDecisionPayload(payload);
			const decision = decisionPayload ? mapApprovalDecisionPayload(decisionPayload) : undefined;
			if (!decision) return message;
			return {
				...message,
				approvals: mergeApprovalDecision(message.approvals, decision),
			};
		}
		case "question.asked": {
			const questionPayload = getQuestionRequestPayload(payload);
			const question = questionPayload
				? mapQuestionRequestPayload(
						questionPayload,
						"assistant_id" in normalizedEvent ? normalizedEvent.assistant_id : undefined,
					)
				: undefined;
			if (!question) return message;
			return {
				...message,
				questions: mergeQuestionRequest(message.questions, question),
			};
		}
		case "plan.published": {
			if (!payload.file_id || !payload.directive) return message;
			const directive = payload.directive;
			// Deduplicate: skip if this file_id already exists in message content.
			if (message.content.includes(directive)) return message;
			return {
				...message,
				content: message.content ? `${message.content}\n${directive}` : directive,
			};
		}
		case "question.answered": {
			const answerPayload = getQuestionAnswerPayload(payload);
			const answer = answerPayload ? mapQuestionAnswerPayload(answerPayload) : undefined;
			if (!answer) return message;
			return {
				...message,
				questions: mergeQuestionAnswer(message.questions, answer),
			};
		}
		case "message.delta":
		case "message.result": {
			const content = getEventContent(normalizedEvent, payload);
			if (!content) return message;

			const shouldAppendProcessStep =
				normalizedEventType === "message.delta" &&
				shouldAppendMessageDeltaToProcess(content, options);
			if (!shouldAppendProcessStep) return message;

			return applyStreamingState(
				{
					...message,
					processSteps: appendProcessThinkingStep(message.processSteps, content),
				},
				options,
			);
		}
		case "reasoning.delta": {
			const thinking = payload.thinking ?? getEventContent(normalizedEvent, payload);
			if (!thinking) return message;
			return applyStreamingState(
				{
					...message,
					processSteps: appendProcessThinkingStep(message.processSteps, thinking),
				},
				options,
			);
		}
		case "tool_call.started":
		case "tool_call.delta":
		case "tool_call.arguments":
		case "tool_call.result":
		case "tool_call.output":
		case "tool_call.completed":
		case "tool_call.failed": {
			const toolCall = mapToolCallEvent(normalizedEventType, payload);
			if (!toolCall) return message;
			return applyStreamingState(
				{
					...message,
					toolCalls: upsertToolCall(message.toolCalls, toolCall),
					processSteps: appendProcessToolCallStep(message.processSteps, toolCall.id),
				},
				options,
			);
		}
		case "run.completed": {
			const resultMessage = getRunResultMessage(payload);
			const metadata = metadataFromPayload(payload);
			const usage = mapUsage(payload.usage ?? payload);
			const artifacts = payload.artifacts
				?.map(mapArtifactPayload)
				.filter((artifact): artifact is MessageArtifact => artifact !== undefined);
			return enrichAssistantMessageMetrics({
				...message,
				status: "completed",
				statusText: undefined,
				content: options.appendContent && resultMessage ? resultMessage : message.content,
				processSteps: pruneFinalContentProcessSteps(message.processSteps, resultMessage),
				todos: completeTodos(message.todos),
				artifacts: artifacts?.length
					? mergeArtifacts(message.artifacts, artifacts)
					: message.artifacts,
				metadata: metadata ? { ...message.metadata, ...metadata } : message.metadata,
				usage: usage ?? message.usage,
			});
		}
		case "run.failed": {
			const failedMessage = getRunFailedMessage(payload);
			if (!failedMessage) return message;
			return {
				...message,
				status: "failed",
				statusText: undefined,
				// 中文注释：失败事件也要回填到当前 assistant 消息里，避免界面只剩空占位。
				content: failedMessage,
			};
		}
		case "run.cancelled": {
			return {
				...message,
				status: "failed",
				statusText: undefined,
				// 取消结果由后端持久化后回拉，避免前端默认文案与后端结果短暂闪烁。
				content: message.content,
			};
		}
		default:
			return message;
	}
}

/**
 * 按顺序将历史 chunks 回放到 Message 上。
 * 供 mapBackendMessage 还原落库消息的执行过程。
 */
export function applySessionEventsToMessage(
	message: Message,
	events: BackendMessageChunk[] | undefined,
	options: ApplySessionEventOptions,
): Message {
	if (!events?.length) return message;
	return events.reduce(
		(current, event) => applySessionEventToMessage(current, event, undefined, options),
		message,
	);
}
