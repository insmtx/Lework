import { FetchSSEClient } from "@leros/ui/lib/fetch-sse";
import { getArtifactDownloadUrl } from "../api/artifactApi";
import { API_BASE_URL } from "../api/config";
import { sessionApi } from "../api/sessionApi";
import type {
	BackendApprovalDecisionPayload,
	BackendApprovalRequestPayload,
	BackendMessage,
	BackendMessageChunk,
	BackendRuntimeTodoItem,
	BackendSessionArtifactPayload,
	BackendSessionEventPayload,
	BackendToolCall,
	SSEMessageEvent,
} from "../api/types";
import { workApi } from "../api/workApi";
import { mockModelOptions } from "../mocks/chatMocks";
import type { SliceCreator } from "../types";
import type {
	ApprovalAction,
	ApprovalRequest,
	Attachment,
	Message,
	MessageArtifact,
	MessageMetadata,
	MessageRole,
	ModelOption,
	RuntimeTodoItem,
	TodoStatus,
	ToolCall,
	ToolCallStatus,
} from "../types/chat";
import { flattenActions } from "../utils";
import { readStoredJwtToken } from "../utils/authStorage";
import { formatFileSize } from "../utils/format";

export type ChatState = {
	messagesMap: Record<string, Message>;
	messageIds: string[];
	streamingMessageId: string | null;
	isGenerating: boolean;
	streamCancelRef: (() => void) | null;

	inputText: string;
	inputAttachments: Attachment[];
	inputFocused: boolean;
	selectedModel: string;
	modelOptions: ModelOption[];
	activeSessionId: string | null;

	tokenUsage: { total: number; currentSession: number };
};

export type ChatAction = Pick<ChatActionImpl, keyof ChatActionImpl>;
export type ChatStore = ChatState & ChatAction;

const _initialState: ChatState = {
	messagesMap: {},
	messageIds: [],
	streamingMessageId: null,
	isGenerating: false,
	streamCancelRef: null,

	inputText: "",
	inputAttachments: [],
	inputFocused: false,
	selectedModel: "gpt-4",
	modelOptions: mockModelOptions,
	activeSessionId: null,

	tokenUsage: { total: 0, currentSession: 0 },
};

type SetState = (
	partial: ChatStore | Partial<ChatStore> | ((state: ChatStore) => ChatStore | Partial<ChatStore>),
	replace?: boolean,
) => void;

type FullStoreGet = () => Record<string, unknown>;

function mapBackendMessage(msg: BackendMessage): Message {
	const message: Message = {
		id: String(msg.id),
		conversationId: msg.session_id ?? msg.conversation_id ?? "",
		role: msg.role as MessageRole,
		content: msg.content ?? "",
		timestamp: msg.timestamp ?? new Date(msg.created_at).getTime(),
		sequence: msg.sequence,
		metadata: mapMetadata(msg.metadata),
	};

	return applySessionEventsToMessage(message, msg.chunks, { appendContent: !message.content });
}

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

function mapMetadata(metadata?: {
	model?: string;
	tokens?: number;
	latency?: number;
}): MessageMetadata | undefined {
	if (!metadata) return undefined;
	return {
		model: metadata.model,
		tokens: metadata.tokens,
		latency: metadata.latency,
	};
}

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

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

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

function getEventPayload(event: NormalizedSessionEvent): BackendSessionEventPayload {
	if (Array.isArray(event.payload)) {
		return { todos: event.payload };
	}
	if (isRecord(event.payload)) {
		return event.payload as BackendSessionEventPayload;
	}
	return event as BackendSessionEventPayload;
}

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

function getRunResultMessage(payload: BackendSessionEventPayload): string | undefined {
	if (typeof payload.message === "string" && payload.message.trim()) {
		return payload.message;
	}
	if (!payload.result || typeof payload.result !== "object") return undefined;
	const value = payload.result as { message?: unknown };
	return typeof value.message === "string" ? value.message : undefined;
}

function metadataFromPayload(payload: BackendSessionEventPayload): MessageMetadata | undefined {
	const metadata = mapMetadata(payload.metadata);
	const tokens = metadata?.tokens ?? payload.usage?.total_tokens ?? payload.total_tokens;
	const model = metadata?.model ?? payload.model;
	if (!tokens && !model && !metadata?.latency) return metadata;
	return {
		...metadata,
		model,
		tokens,
	};
}

function mergeToolCalls(current: ToolCall[] | undefined, updates: ToolCall[]): ToolCall[] {
	return updates.reduce((acc, update) => upsertToolCall(acc, update), current ?? []);
}

function getTodoItemsFromValue(value: unknown): BackendRuntimeTodoItem[] | undefined {
	if (!Array.isArray(value)) return undefined;
	if (!value.every(isRecord)) return undefined;
	return value as BackendRuntimeTodoItem[];
}

function mapTodoItems(items: BackendRuntimeTodoItem[]): RuntimeTodoItem[] {
	return items.map((item, index) => ({
		id: item.id?.trim() || `todo-${index + 1}`,
		title: item.title?.trim() || `待办 ${index + 1}`,
		status: normalizeTodoStatus(item.status),
		priority: item.priority,
	}));
}

function mapArtifactPayload(payload: BackendSessionArtifactPayload): MessageArtifact | undefined {
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
		updatedAt: "",
		downloadUrl: getArtifactDownloadUrl(artifactID),
		sha256: payload.sha256,
	};
}

function mergeArtifacts(
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

function getApprovalStatus(action?: string): ApprovalRequest["status"] {
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

function mapApprovalRequestPayload(
	payload: BackendApprovalRequestPayload,
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
	};
}

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

function getApprovalRequestPayload(
	payload: BackendSessionEventPayload,
): BackendApprovalRequestPayload | undefined {
	if (payload.approval_request) return payload.approval_request;
	if (payload.request_id || payload.tool_name) return payload;
	return undefined;
}

function getApprovalDecisionPayload(
	payload: BackendSessionEventPayload,
): BackendApprovalDecisionPayload | undefined {
	if (payload.approval_decision) return payload.approval_decision;
	if (payload.request_id || payload.action) return payload;
	return undefined;
}

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

function applySessionEventToMessage(
	message: Message,
	event: SessionEventLike,
	eventType: string | undefined,
	options: { appendContent: boolean },
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
			return { ...message, toolCalls: mergeToolCalls(message.toolCalls, toolCalls) };
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
			return { ...message, artifacts: mergeArtifacts(message.artifacts, [artifact]) };
		}
		case "approval.requested": {
			const approvalPayload = getApprovalRequestPayload(payload);
			const approval = approvalPayload ? mapApprovalRequestPayload(approvalPayload) : undefined;
			if (!approval) return message;
			return { ...message, approvals: mergeApprovalRequest(message.approvals, approval) };
		}
		case "approval.resolved": {
			const decisionPayload = getApprovalDecisionPayload(payload);
			const decision = decisionPayload ? mapApprovalDecisionPayload(decisionPayload) : undefined;
			if (!decision) return message;
			return { ...message, approvals: mergeApprovalDecision(message.approvals, decision) };
		}
		case "message.delta":
		case "message.result": {
			const content = getEventContent(normalizedEvent, payload);
			if (!content || !options.appendContent) return message;
			return { ...message, content: message.content + content };
		}
		case "reasoning.delta": {
			const thinking = payload.thinking ?? getEventContent(normalizedEvent, payload);
			if (!thinking) return message;
			return { ...message, thinking: (message.thinking ?? "") + thinking };
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
			return { ...message, toolCalls: upsertToolCall(message.toolCalls, toolCall) };
		}
		case "run.completed": {
			const resultMessage = getRunResultMessage(payload);
			const metadata = metadataFromPayload(payload);
			const artifacts = payload.artifacts
				?.map(mapArtifactPayload)
				.filter((artifact): artifact is MessageArtifact => artifact !== undefined);
			return {
				...message,
				content:
					options.appendContent && !message.content && resultMessage
						? resultMessage
						: message.content,
				artifacts: artifacts?.length
					? mergeArtifacts(message.artifacts, artifacts)
					: message.artifacts,
				metadata: metadata ? { ...message.metadata, ...metadata } : message.metadata,
			};
		}
		default:
			return message;
	}
}

function applySessionEventsToMessage(
	message: Message,
	events: BackendMessageChunk[] | undefined,
	options: { appendContent: boolean },
): Message {
	if (!events?.length) return message;
	return events.reduce(
		(current, event) => applySessionEventToMessage(current, event, undefined, options),
		message,
	);
}

function isOptimisticMessage(message: Message): boolean {
	return message.id.startsWith("msg-user-") || message.id.startsWith("msg-assistant-");
}

function normalizedMessageContent(message: Message): string {
	return message.content.trim().replace(/\s+/g, " ");
}

function messageMergeKey(message: Message): string | undefined {
	const content = normalizedMessageContent(message);
	if (!content) return undefined;
	return `${message.role}:${content}`;
}

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

function shouldKeepLocalMessage(
	persistedMessages: Message[],
	localMessages: Message[],
	localMessage: Message,
	localIndex: number,
): boolean {
	if (persistedMessages.some((message) => message.id === localMessage.id)) return false;
	if (!isOptimisticMessage(localMessage)) return true;
	if (!messageMergeKey(localMessage)) return true;

	const localOccurrence = countMatchingMessages(localMessages, localMessage, localIndex);
	const persistedOccurrence = countMatchingMessages(persistedMessages, localMessage);
	return persistedOccurrence < localOccurrence;
}

function compareMessages(a: Message, b: Message): number {
	if (a.sequence !== undefined && b.sequence !== undefined) {
		return a.sequence - b.sequence;
	}
	return a.timestamp - b.timestamp;
}

function mergeSessionMessages(persistedMessages: Message[], localMessages: Message[]): Message[] {
	const merged = [...persistedMessages];
	localMessages.forEach((localMessage, index) => {
		if (!shouldKeepLocalMessage(persistedMessages, localMessages, localMessage, index)) {
			return;
		}
		if (merged.some((message) => message.id === localMessage.id)) return;
		merged.push(localMessage);
	});
	return merged.sort(compareMessages);
}

export class ChatActionImpl {
	readonly #set: SetState;
	readonly #get: () => ChatStore;
	readonly #fullGet: FullStoreGet;
	#sseClient: FetchSSEClient | null = null;

	constructor(set: SetState, get: () => ChatStore, fullGet: FullStoreGet) {
		this.#set = set;
		this.#get = get;
		this.#fullGet = fullGet;
	}

	#dispatchChat = (action: ChatActionType) => {
		this.#set((state) => chatReducer(state, action));
	};

	setActiveSession = (sessionId: string) => {
		this.#set({ activeSessionId: sessionId });
	};

	sendMessage = async (content: string, attachments?: Attachment[]) => {
		if (!content.trim() && !attachments?.length) return;

		const state = this.#get();
		let { activeSessionId } = state;

		if (!activeSessionId) {
			try {
				const res = await sessionApi.create({ type: "chat", title: "新会话" });
				const session = res.data.data;
				if (!session) return;
				activeSessionId = session.session_id;
				const conv = {
					id: session.session_id,
					title: session.title || "未命名会话",
					sessionId: session.session_id,
					type: session.type,
					status: session.status,
					createdAt: new Date(session.created_at).getTime(),
					updatedAt: new Date(session.updated_at).getTime(),
				};
				const prevState = this.#fullGet() as {
					conversations: Array<typeof conv>;
					activeConversationId: string | null;
					conversationsLoaded: boolean;
				};
				(this.#set as (partial: Record<string, unknown>) => void)({
					activeSessionId,
					conversations: [conv, ...prevState.conversations],
					activeConversationId: conv.id,
					conversationsLoaded: true,
				});
			} catch (err) {
				console.error("Auto-create conversation error:", err);
				return;
			}
		}

		try {
			await sessionApi.addMessage({
				session_id: activeSessionId,
				role: "user",
				content,
				message_type: "text",
			});
		} catch (err) {
			console.error("sendMessage addMessage error:", err);
			return;
		}

		const now = Date.now();
		const userMsg: Message = {
			id: `msg-user-${now}`,
			conversationId: activeSessionId,
			role: "user",
			content,
			timestamp: now,
		};

		const assistantMsg: Message = {
			id: `msg-assistant-${now}`,
			conversationId: activeSessionId,
			role: "assistant",
			content: "",
			timestamp: now + 100,
		};

		this.#dispatchChat({ type: "addMessage", value: userMsg });
		this.#dispatchChat({ type: "addMessage", value: assistantMsg });
		this.#set({
			streamingMessageId: assistantMsg.id,
			isGenerating: true,
			inputText: "",
			inputAttachments: [],
		});

		this.#startSSE(activeSessionId, assistantMsg.id);
	};

	sendProjectMessage = async (content: string, projectId?: string | null) => {
		const trimmed = content.trim();
		if (!trimmed || !projectId) return;

		try {
			const res = await workApi.newMessage({ content: trimmed, project_id: projectId });
			const data = res.data.data;
			if (!data?.project_id || !data?.task_id || !data?.session_id) return;

			(this.#set as (partial: Record<string, unknown>) => void)({
				activeProjectId: data.project_id,
				activeTaskDetailProjectId: data.project_id,
				activeTaskDetailTaskId: data.task_id,
				activeTaskDetailSessionId: data.session_id,
				currentView: "taskDetail",
				activeProjectTab: "chat",
				conversationListOpen: false,
				inputText: "",
				inputAttachments: [],
			});

			await this.startSessionResponseStream(data.session_id, trimmed);

			const fullState = this.#fullGet() as {
				fetchProjectDetail?: (projectId: string) => Promise<void>;
			};
			await fullState.fetchProjectDetail?.(data.project_id);
		} catch (err) {
			console.error("sendProjectMessage error:", err);
		}
	};

	startSessionResponseStream = async (sessionId: string, content: string) => {
		const trimmed = content.trim();
		if (!sessionId || !trimmed) return;

		const state = this.#get();
		if (state.activeSessionId === sessionId && state.isGenerating) return;

		const now = Date.now();
		this.#set({
			activeSessionId: sessionId,
			streamingMessageId: null,
			isGenerating: true,
			inputText: "",
			inputAttachments: [],
		});

		const fallbackUserMsg: Message = {
			id: `msg-user-${now}`,
			conversationId: sessionId,
			role: "user",
			content: trimmed,
			timestamp: now,
		};
		const assistantMsg: Message = {
			id: `msg-assistant-${now}`,
			conversationId: sessionId,
			role: "assistant",
			content: "",
			timestamp: now + 100,
		};

		let messages: Message[] = [fallbackUserMsg, assistantMsg];
		try {
			const res = await sessionApi.getMessages(sessionId, 1, 100);
			const persistedMessages = (res.data.data?.items ?? []).map(mapBackendMessage);
			messages = mergeSessionMessages(persistedMessages, [fallbackUserMsg]);
			messages.push(assistantMsg);
		} catch (err) {
			console.error("startSessionResponseStream load history error:", err);
		}

		const messagesMap: Record<string, Message> = {};
		const messageIds: string[] = [];
		for (const message of messages) {
			messagesMap[message.id] = message;
			messageIds.push(message.id);
		}

		this.#set({
			activeSessionId: sessionId,
			messagesMap,
			messageIds,
			streamingMessageId: assistantMsg.id,
			isGenerating: true,
			inputText: "",
			inputAttachments: [],
		});

		this.#startSSE(sessionId, assistantMsg.id);
	};

	#startSSE = (sessionId: string, assistantMsgId: string) => {
		if (this.#sseClient) {
			this.#sseClient.close();
			this.#sseClient = null;
		}

		const url = `${API_BASE_URL}/SessionEvents`;
		const token = readStoredJwtToken();
		const client = new FetchSSEClient(url, {
			method: "POST",
			headers: token ? { Authorization: `Bearer ${token}` } : undefined,
			body: { session_id: sessionId },
			onMessage: (event) => {
				try {
					const data = JSON.parse(event.data) as SSEMessageEvent;
					const eventType = event.type ?? data.type;

					const msg = this.#get().messagesMap[assistantMsgId];
					if (msg) {
						const nextMsg = applySessionEventToMessage(msg, data, eventType, {
							appendContent: true,
						});
						if (nextMsg !== msg) {
							this.#dispatchChat({
								type: "updateMessage",
								id: assistantMsgId,
								value: nextMsg,
							});
						}
					}

					if (eventType === "run.completed" || eventType === "run.failed") {
						this.#finishStream();
						this.#sseClient?.close();
						this.#sseClient = null;
					}
				} catch {
					const msg = this.#get().messagesMap[assistantMsgId];
					if (msg && event.data) {
						this.#dispatchChat({
							type: "updateMessage",
							id: assistantMsgId,
							value: { ...msg, content: msg.content + event.data },
						});
					}
				}
			},
			onError: (err) => {
				console.error("SSE error:", err);
				this.#finishStream();
			},
		});

		this.#set({ streamCancelRef: () => client.close() });
		client.connect();
		this.#sseClient = client;
	};

	#finishStream = () => {
		this.#set({
			streamingMessageId: null,
			isGenerating: false,
			streamCancelRef: null,
		});
	};

	cancelGeneration = () => {
		const state = this.#get();
		state.streamCancelRef?.();
		const streamingId = state.streamingMessageId;
		if (streamingId) {
			const msg = state.messagesMap[streamingId];
			if (msg) {
				this.#dispatchChat({
					type: "updateMessage",
					id: streamingId,
					value: { ...msg },
				});
			}
		}
		if (this.#sseClient) {
			this.#sseClient.close();
			this.#sseClient = null;
		}
		this.#finishStream();
	};

	loadConversationMessages = async (sessionId: string) => {
		try {
			const res = await sessionApi.getMessages(sessionId, 1, 100);
			const items = res.data.data?.items ?? [];
			const persistedMessages = items.map(mapBackendMessage);
			const state = this.#get();
			const shouldPreserveLocalMessages =
				state.activeSessionId === sessionId ||
				(state.isGenerating &&
					state.streamingMessageId !== null &&
					state.messagesMap[state.streamingMessageId]?.conversationId === sessionId);
			const localSessionMessages = shouldPreserveLocalMessages
				? state.messageIds
						.map((id) => state.messagesMap[id])
						.filter((message): message is Message => message?.conversationId === sessionId)
				: [];
			const messages = localSessionMessages.length
				? mergeSessionMessages(persistedMessages, localSessionMessages)
				: persistedMessages;

			const maps: Record<string, Message> = {};
			const ids: string[] = [];
			for (const m of messages) {
				maps[m.id] = m;
				ids.push(m.id);
			}

			this.#set({ messagesMap: maps, messageIds: ids });
		} catch (err) {
			console.error("loadConversationMessages error:", err);
			if (this.#get().activeSessionId !== sessionId) {
				this.#set({ messagesMap: {}, messageIds: [] });
			}
		}
	};

	resetLocalMessages = () => {
		if (this.#sseClient) {
			this.#sseClient.close();
			this.#sseClient = null;
		}
		this.#set({
			messagesMap: {},
			messageIds: [],
			activeSessionId: null,
			streamingMessageId: null,
			isGenerating: false,
			streamCancelRef: null,
		});
	};

	setInputText = (text: string) => {
		this.#set({ inputText: text });
	};

	addAttachment = (file: File) => {
		const id = `att-${Date.now()}`;
		const url = URL.createObjectURL(file);
		const attachment: Attachment = {
			id,
			type: file.type.startsWith("image/") ? "image" : "file",
			name: file.name,
			size: file.size,
			url,
			file,
		};
		this.#set((state) => ({
			inputAttachments: [...state.inputAttachments, attachment],
		}));
	};

	removeAttachment = (id: string) => {
		const state = this.#get();
		const att = state.inputAttachments.find((a) => a.id === id);
		if (att?.url) URL.revokeObjectURL(att.url);
		this.#set((state) => ({
			inputAttachments: state.inputAttachments.filter((a) => a.id !== id),
		}));
	};

	setInputFocused = (focused: boolean) => {
		this.#set({ inputFocused: focused });
	};

	setSelectedModel = (modelId: string) => {
		this.#set({ selectedModel: modelId });
	};

	resendMessage = async (messageId: string) => {
		const state = this.#get();
		const oldMsg = state.messagesMap[messageId];
		if (oldMsg?.role !== "assistant") return;

		const { activeSessionId } = state;
		if (!activeSessionId) return;

		const now = Date.now();
		const newMsg: Message = {
			id: `msg-assistant-${now}`,
			conversationId: oldMsg.conversationId,
			role: "assistant",
			content: "",
			timestamp: now,
		};

		this.#dispatchChat({ type: "addMessage", value: newMsg });
		this.#set({
			streamingMessageId: newMsg.id,
			isGenerating: true,
		});

		this.#startSSE(activeSessionId, newMsg.id);
	};

	submitApprovalDecision = async (
		messageId: string,
		requestId: string,
		action: ApprovalAction,
		reason?: string,
	) => {
		const state = this.#get();
		const message = state.messagesMap[messageId];
		const sessionId = message?.conversationId || state.activeSessionId;
		if (!sessionId) return;

		this.#dispatchChat({
			type: "updateApprovalStatus",
			messageId,
			requestId,
			status: "submitting",
			action,
			reason,
			error: undefined,
		});

		try {
			await sessionApi.submitApprovalDecision({
				session_id: sessionId,
				request_id: requestId,
				action,
				reason,
			});
			this.#dispatchChat({
				type: "updateApprovalStatus",
				messageId,
				requestId,
				status: getApprovalStatus(action),
				action,
				reason,
				error: undefined,
			});
		} catch (err) {
			console.error("submitApprovalDecision error:", err);
			this.#dispatchChat({
				type: "updateApprovalStatus",
				messageId,
				requestId,
				status: "error",
				action,
				reason,
				error: "提交审批失败，请重试",
			});
		}
	};

	deleteMessage = async (messageId: number) => {
		try {
			await sessionApi.deleteMessage(messageId);
			this.#dispatchChat({ type: "removeMessage", id: String(messageId) });
		} catch (err) {
			console.error("deleteMessage error:", err);
		}
	};

	clearSessionMessages = async (sessionId: string) => {
		try {
			await sessionApi.clearMessages(sessionId);
			this.#set({ messagesMap: {}, messageIds: [] });
		} catch (err) {
			console.error("clearSessionMessages error:", err);
		}
	};
}

type ChatActionType =
	| { type: "addMessage"; value: Message }
	| { type: "updateMessage"; id: string; value: Message }
	| { type: "removeMessage"; id: string }
	| {
			type: "updateApprovalStatus";
			messageId: string;
			requestId: string;
			status: ApprovalRequest["status"];
			action?: ApprovalAction;
			reason?: string;
			error?: string;
	  }
	| {
			type: "updateToolCallStatus";
			toolCallId: string;
			status: ToolCallStatus;
			result?: Record<string, unknown>;
	  };

function chatReducer(state: ChatState, action: ChatActionType): ChatState {
	switch (action.type) {
		case "addMessage": {
			const msg = action.value;
			return {
				...state,
				messagesMap: { ...state.messagesMap, [msg.id]: msg },
				messageIds: [...state.messageIds, msg.id],
			};
		}

		case "updateMessage": {
			const { id, value } = action;
			if (!state.messagesMap[id]) return state;
			return {
				...state,
				messagesMap: { ...state.messagesMap, [id]: value },
			};
		}

		case "removeMessage": {
			const { id } = action;
			const { [id]: _, ...remainingMaps } = state.messagesMap;
			return {
				...state,
				messagesMap: remainingMaps,
				messageIds: state.messageIds.filter((mid) => mid !== id),
			};
		}

		case "updateApprovalStatus": {
			const { messageId, requestId, status, action: approvalAction, reason, error } = action;
			const msg = state.messagesMap[messageId];
			if (!msg?.approvals) return state;

			const updatedApprovals = msg.approvals.map((approval) =>
				approval.requestId === requestId
					? {
							...approval,
							status,
							action: approvalAction ?? approval.action,
							reason: reason ?? approval.reason,
							error,
						}
					: approval,
			);

			return {
				...state,
				messagesMap: {
					...state.messagesMap,
					[messageId]: { ...msg, approvals: updatedApprovals },
				},
			};
		}

		case "updateToolCallStatus": {
			const { toolCallId, status, result } = action;
			const msgId =
				state.streamingMessageId ??
				state.messageIds.find((id) => {
					const msg = state.messagesMap[id];
					return msg?.toolCalls?.some((tc) => tc.id === toolCallId);
				});

			if (!msgId) return state;
			const msg = state.messagesMap[msgId];
			if (!msg?.toolCalls) return state;

			const updatedToolCalls = msg.toolCalls.map((tc) =>
				tc.id === toolCallId ? { ...tc, status, ...(result ? { result } : {}) } : tc,
			);

			return {
				...state,
				messagesMap: {
					...state.messagesMap,
					[msgId]: { ...msg, toolCalls: updatedToolCalls },
				},
			};
		}

		default:
			return state;
	}
}

export const chatSlice: SliceCreator<ChatStore> = (...params) => ({
	..._initialState,
	...flattenActions<ChatAction>([
		new ChatActionImpl(
			params[0] as SetState,
			params[1] as () => ChatStore,
			params[1] as FullStoreGet,
		),
	]),
});
