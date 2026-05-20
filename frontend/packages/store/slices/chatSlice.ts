import { FetchSSEClient } from "@leros/ui/lib/fetch-sse";
import { API_BASE_URL } from "../api/config";
import { sessionApi } from "../api/sessionApi";
import type { BackendMessage, BackendToolCall, SSEMessageEvent } from "../api/types";
import { mockModelOptions } from "../mocks/chatMocks";
import type { SliceCreator } from "../types";
import type {
	Attachment,
	Message,
	MessageRole,
	ModelOption,
	ToolCall,
	ToolCallStatus,
} from "../types/chat";
import { flattenActions } from "../utils";

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
	return {
		id: String(msg.id),
		conversationId: msg.conversation_id,
		role: msg.role as MessageRole,
		content: msg.content ?? "",
		timestamp: msg.timestamp ?? new Date(msg.created_at).getTime(),
		metadata: msg.metadata
			? {
					model: msg.metadata.model,
					tokens: msg.metadata.tokens,
					latency: msg.metadata.latency,
				}
			: undefined,
	};
}

function mapToolCalls(tcList?: BackendToolCall[]): ToolCall[] | undefined {
	if (!tcList) return undefined;
	return tcList.map((tc) => ({
		id: tc.id,
		name: tc.name,
		arguments: tc.arguments ?? {},
		status: tc.status as ToolCallStatus,
		result: tc.result,
		duration: tc.duration,
	}));
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
					sessionDbId: session.id,
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

	#startSSE = (sessionId: string, assistantMsgId: string) => {
		if (this.#sseClient) {
			this.#sseClient.close();
			this.#sseClient = null;
		}

		const url = `${API_BASE_URL}/SessionEvents`;
		const client = new FetchSSEClient(url, {
			method: "POST",
			body: { session_id: sessionId },
			onMessage: (event) => {
				try {
					const data = JSON.parse(event.data) as SSEMessageEvent;
					const eventType = event.type ?? data.type;

					if (eventType === "message.delta") {
						const payload = data.payload ?? data;
						const content = payload.content ?? data.content ?? data.chunk;
						if (content) {
							const msg = this.#get().messagesMap[assistantMsgId];
							if (msg) {
								this.#dispatchChat({
									type: "updateMessage",
									id: assistantMsgId,
									value: { ...msg, content: msg.content + content },
								});
							}
						}
					} else if (eventType === "message.result") {
						const payload = data.payload ?? data;
						const content = payload.content ?? data.content;
						if (content) {
							const msg = this.#get().messagesMap[assistantMsgId];
							if (msg) {
								this.#dispatchChat({
									type: "updateMessage",
									id: assistantMsgId,
									value: { ...msg, content: msg.content + content },
								});
							}
						}
					} else if (eventType === "run.completed") {
						const msg = this.#get().messagesMap[assistantMsgId];
						if (msg) {
							this.#dispatchChat({
								type: "updateMessage",
								id: assistantMsgId,
								value: {
									...msg,
									thinking: data.thinking ?? msg.thinking,
									toolCalls: mapToolCalls(data.tool_calls ?? data.payload?.tool_calls),
									metadata: data.metadata
										? {
												model: data.metadata.model,
												tokens: data.metadata.tokens,
												latency: data.metadata.latency,
											}
										: msg.metadata,
								},
							});
						}
						this.#finishStream();
						this.#sseClient?.close();
						this.#sseClient = null;
					} else if (eventType === "run.failed") {
						const msg = this.#get().messagesMap[assistantMsgId];
						if (msg) {
							this.#dispatchChat({
								type: "updateMessage",
								id: assistantMsgId,
								value: { ...msg },
							});
						}
						this.#finishStream();
						this.#sseClient?.close();
						this.#sseClient = null;
					} else if (
						eventType === "tool_call.started" ||
						eventType === "tool_call.arguments" ||
						eventType === "tool_call.output" ||
						eventType === "tool_call.completed" ||
						eventType === "tool_call.failed"
					) {
						if (data.tool_calls ?? data.payload?.tool_calls) {
							const msg = this.#get().messagesMap[assistantMsgId];
							if (msg) {
								this.#dispatchChat({
									type: "updateMessage",
									id: assistantMsgId,
									value: {
										...msg,
										toolCalls: mapToolCalls(data.tool_calls ?? data.payload?.tool_calls),
									},
								});
							}
						}
					} else if (eventType === "reasoning.delta") {
						const payload = data.payload ?? data;
						if (payload.content ?? data.content) {
							const msg = this.#get().messagesMap[assistantMsgId];
							if (msg) {
								this.#dispatchChat({
									type: "updateMessage",
									id: assistantMsgId,
									value: {
										...msg,
										thinking: (msg.thinking ?? "") + (payload.content ?? data.content),
									},
								});
							}
						}
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
			const messages = items.map(mapBackendMessage);

			const maps: Record<string, Message> = {};
			const ids: string[] = [];
			for (const m of messages) {
				maps[m.id] = m;
				ids.push(m.id);
			}

			this.#set({ messagesMap: maps, messageIds: ids });
		} catch (err) {
			console.error("loadConversationMessages error:", err);
			this.#set({ messagesMap: {}, messageIds: [] });
		}
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
		if (!oldMsg || oldMsg.role !== "assistant") return;

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

	deleteMessage = async (messageId: number) => {
		try {
			await sessionApi.deleteMessage(messageId);
			this.#dispatchChat({ type: "removeMessage", id: String(messageId) });
		} catch (err) {
			console.error("deleteMessage error:", err);
		}
	};

	clearMessages = async (sessionId: string) => {
		try {
			await sessionApi.clearMessages(sessionId);
			this.#set({ messagesMap: {}, messageIds: [] });
		} catch (err) {
			console.error("clearMessages error:", err);
		}
	};
}

type ChatActionType =
	| { type: "addMessage"; value: Message }
	| { type: "updateMessage"; id: string; value: Message }
	| { type: "removeMessage"; id: string }
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
