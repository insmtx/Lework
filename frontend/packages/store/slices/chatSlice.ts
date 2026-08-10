/**
 * 对话 Zustand slice（facade / 编排层）。
 *
 * 本文件只负责：装配 SessionStream / GlobalEvents / HistoryLoader / Composer / send deps，
 * 对外暴露与旧版兼容的 ChatStore 方法，以及 re-export 纯函数。
 * 业务实现已下沉到 `../chat/*`（勿在本文件新增大段协议/映射逻辑）。
 */
import { sessionApi } from "../api/sessionApi";
import {
	allLocalMessagesBelongToSession,
	type ChatState,
	getApprovalStatus,
	initialChatState,
	isClientReplyTimeoutMessage,
	resolveActiveRunIdForCancel,
	retainLocalMessagesForSession,
} from "../chat";
import { Composer } from "../chat/composer";
import { ChatEffects } from "../chat/effects";
import { GlobalEventsManager } from "../chat/globalEvents";
import { HistoryLoader } from "../chat/historyLoader";
import {
	bootstrapNewTaskSession as bootstrapNewTaskSessionImpl,
	type SendPipelineDeps,
	type SendProjectMessageOptions,
	sendProjectMessage as sendProjectMessageImpl,
	sendTaskRoomMessage as sendTaskRoomMessageImpl,
} from "../chat/send";
import { SessionStream } from "../chat/sessionStream";
import type { SliceCreator } from "../types";
import type {
	ApprovalAction,
	ApprovalRequest,
	Attachment,
	ExecutionMode,
	Message,
	MessageMetadata,
	QuestionRequest,
	ToolCallStatus,
} from "../types/chat";
import { flattenActions } from "../utils";

export type { ChatState } from "../chat";
export {
	allLocalMessagesBelongToSession,
	applySessionEventToMessage,
	attachAssistantReplyTargets,
	createAssistantSessionEventsWaitingMessage,
	insertGlobalUserMessageId,
	isGlobalUserEchoMessage,
	isTaskRoomAssistantPlaceholder,
	mapBackendMessage,
	retainLocalMessagesForSession,
} from "../chat";

export type ChatAction = Pick<ChatActionImpl, keyof ChatActionImpl>;
export type ChatStore = ChatState & ChatAction;

const _initialState: ChatState = initialChatState;

type SetState = (
	partial: ChatStore | Partial<ChatStore> | ((state: ChatStore) => ChatStore | Partial<ChatStore>),
	replace?: boolean,
) => void;

type FullStoreGet = () => Record<string, unknown>;

/**
 * 解析某个 session 所属的 projectId（优先任务详情页上下文，否则扫 projects 列表）。
 * 供 SessionStream 终态刷新项目详情定位项目。
 */
function resolveProjectIdForSession(
	fullState: {
		activeTaskDetailProjectId?: string | null;
		projects?: Array<{ id: string; tasks: Array<{ sessionId?: string }> }>;
	},
	sessionId: string,
): string | null {
	if (fullState.activeTaskDetailProjectId) {
		return fullState.activeTaskDetailProjectId;
	}
	for (const project of fullState.projects ?? []) {
		if (project.tasks.some((task) => task.sessionId === sessionId)) {
			return project.id;
		}
	}
	return null;
}

export class ChatActionImpl {
	readonly #set: SetState;
	readonly #get: () => ChatStore;
	readonly #fullGet: FullStoreGet;
	readonly #sessionStream: SessionStream;
	readonly #globalEvents: GlobalEventsManager;
	readonly #historyLoader: HistoryLoader;
	readonly #composer: Composer;
	readonly #effects: ChatEffects;
	readonly #sendDeps: SendPipelineDeps;

	constructor(set: SetState, get: () => ChatStore, fullGet: FullStoreGet) {
		this.#set = set;
		this.#get = get;
		this.#fullGet = fullGet;

		/** 部分更新 ChatState（兼容 Zustand set 的函数式写法）。 */
		const setChat = (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => {
			if (typeof partial === "function") {
				this.#set((state) => partial(state));
				return;
			}
			this.#set(partial);
		};

		/** 写入联合 store 任意字段（layout + chat）。 */
		const setStore = (partial: Record<string, unknown>) => {
			(this.#set as (p: Record<string, unknown>) => void)(partial);
		};

		this.#effects = new ChatEffects({
			setStore,
			fullGet: () => this.#fullGet(),
		});

		this.#composer = new Composer({
			get: () => this.#get(),
			set: setChat,
		});

		const refreshProjectForSession = (sessionId: string) => {
			const fullState = this.#fullGet() as {
				activeTaskDetailProjectId?: string | null;
				fetchProjectDetail?: (projectId: string) => Promise<void>;
				projects?: Array<{ id: string; tasks: Array<{ sessionId?: string }> }>;
			};
			const projectId = resolveProjectIdForSession(fullState, sessionId);
			if (projectId) {
				void fullState.fetchProjectDetail?.(projectId);
			}
		};

		this.#sessionStream = new SessionStream({
			get: () => this.#get(),
			set: setChat,
			updateMessage: (id, value) => this.#dispatchChat({ type: "updateMessage", id, value }),
			loadConversationMessages: (sessionId, options) =>
				this.#historyLoader.load(sessionId, options),
			fullGet: () => this.#fullGet(),
			refreshProjectForSession,
		});

		this.#historyLoader = new HistoryLoader({
			get: () => this.#get(),
			set: setChat,
			startSessionStream: (sessionId, assistantMsgId, replay, assistantId) =>
				this.#sessionStream.start(sessionId, assistantMsgId, replay, assistantId),
			finishStream: () => this.#sessionStream.finish(),
		});

		this.#globalEvents = new GlobalEventsManager({
			get: () => this.#get(),
			set: setChat,
			fullGet: () => this.#fullGet(),
			addMessage: (message) => this.#dispatchChat({ type: "addMessage", value: message }),
			startSessionStream: (sessionId, assistantMsgId, replay, assistantId) =>
				this.#sessionStream.start(sessionId, assistantMsgId, replay, assistantId),
			finishStream: () => this.#sessionStream.finish(),
			loadConversationMessages: (sessionId, options) =>
				this.#historyLoader.load(sessionId, options),
			refreshProjectForSession,
		});

		this.#sendDeps = {
			get: () => this.#get(),
			set: setChat,
			setStore,
			fullGet: () => this.#fullGet(),
			addMessage: (message) => this.#dispatchChat({ type: "addMessage", value: message }),
			updateMessage: (id, value) => this.#dispatchChat({ type: "updateMessage", id, value }),
			startGlobalEvents: () => this.#globalEvents.start(),
			drainGlobalEvents: (sessionId) => this.#globalEvents.drain(sessionId),
			startSessionStream: (sessionId, assistantMsgId, replay, assistantId) =>
				this.#sessionStream.start(sessionId, assistantMsgId, replay, assistantId),
			finishStream: () => this.#sessionStream.finish(),
			loadConversationMessages: (sessionId, options) =>
				this.#historyLoader.load(sessionId, options),
			effects: this.#effects,
		};
	}

	#dispatchChat = (action: ChatActionType) => {
		this.#set((state) => chatReducer(state, action));
	};

	setActiveSession = (sessionId: string) => {
		const state = this.#get();
		const switchingSession = state.activeSessionId !== sessionId;

		if (switchingSession) {
			this.#sessionStream.closeIfBoundToOtherSession(sessionId);
			this.#set((current) => retainLocalMessagesForSession(current, sessionId));
		}

		this.#set({ activeSessionId: sessionId });
		this.#globalEvents.drain(sessionId);
	};

	allMessagesBelongToSession = (sessionId: string) => {
		return allLocalMessagesBelongToSession(this.#get(), sessionId);
	};

	/** 新建任务跳转前写入等待占位（实现见 send/bootstrap）。 */
	bootstrapNewTaskSession = (
		sessionId: string,
		content: string,
		options?: {
			attachments?: Attachment[];
			metadata?: MessageMetadata;
		},
	) => {
		bootstrapNewTaskSessionImpl(this.#sendDeps, sessionId, content, options);
	};

	// 中文注释：bootstrap 标记会阻止历史加载；仅在消息已被页面卸载清理时手动清除。
	clearPendingBootstrapSession = () => {
		if (!this.#get().pendingBootstrapSessionId) return;
		this.#set({ pendingBootstrapSessionId: null });
	};

	hasSessionMessages = (sessionId: string) => {
		const state = this.#get();
		return state.messageIds.some((id) => state.messagesMap[id]?.conversationId === sessionId);
	};

	/** 幂等启动 GlobalEvents 长连接（实现见 GlobalEventsManager）。 */
	startGlobalEvents = async () => {
		return this.#globalEvents.start();
	};

	/** 关闭 GlobalEvents 长连接。 */
	stopGlobalEvents = () => {
		this.#globalEvents.stop();
	};

	/** 任务群聊续聊（实现见 send/sendTaskRoomMessage）。 */
	sendTaskRoomMessage = async (
		content: string,
		params: {
			projectId: string;
			taskId: string;
			sessionId: string;
			metadata?: MessageMetadata;
			connectorIds?: string[];
		},
		attachments?: Attachment[],
	) => {
		return sendTaskRoomMessageImpl(this.#sendDeps, content, params, attachments);
	};

	/**
	 * 项目首页 / 工作台新建任务（实现见 send/sendProjectMessage）。
	 * options 供工作台透传 assistantIds / await 详情等，不改变 ChatInput 原有四参调用。
	 */
	sendProjectMessage = async (
		content: string,
		projectId?: string | null,
		attachments?: Attachment[],
		metadata?: MessageMetadata,
		options?: SendProjectMessageOptions,
	) => {
		return sendProjectMessageImpl(
			this.#sendDeps,
			content,
			projectId,
			attachments,
			metadata,
			options,
		);
	};

	/** 打开 SessionEvents（实现见 SessionStream）。 */
	#startSSE = (sessionId: string, assistantMsgId: string, replay = false, assistantId?: string) => {
		void this.#sessionStream.start(sessionId, assistantMsgId, replay, assistantId);
	};

	cancelGeneration = () => {
		const state = this.#get();
		if (state.activeSessionId && state.cancellingSessionId === state.activeSessionId) return;
		const runId = resolveActiveRunIdForCancel(state);
		const streamingMessage = state.streamingMessageId
			? state.messagesMap[state.streamingMessageId]
			: undefined;
		// 等待态尚未分配 worker/run，发送取消会被后端视为无活跃任务；
		// 等待 GlobalEvents 绑定 run 后再允许取消，避免前端进入无法完成的取消态。
		if (streamingMessage?.status === "waiting" && !runId) return;

		// 标记此 session 正在取消 + 通知后端真实取消 agent 执行。
		// 保持当前生成态和 SSE 连接，直至收到 run.cancelled：这会阻止新消息
		// 插入仍在收尾的会话，也避免迟到的 GlobalEvents 重建 assistant 占位。
		if (state.activeSessionId) {
			this.#set({ cancellingSessionId: state.activeSessionId });
			sessionApi
				.cancelSessionRun({
					session_id: state.activeSessionId,
					...(runId ? { run_id: runId } : {}),
				})
				.catch(() => {
					// 取消请求失败时继续等待原始 run，不保留取消标记以免屏蔽正常流事件。
					this.#set((current) =>
						current.cancellingSessionId === state.activeSessionId
							? { cancellingSessionId: null }
							: {},
					);
				});
		}

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
	};

	/** 加载会话历史 / 进页 resume（实现见 HistoryLoader）。 */
	loadConversationMessages = async (sessionId: string, options?: { resumeStream?: boolean }) => {
		return this.#historyLoader.load(sessionId, options);
	};

	resetLocalMessages = () => {
		this.#sessionStream.close();
		this.#set({
			messagesMap: {},
			messageIds: [],
			activeSessionId: null,
			streamingMessageId: null,
			isGenerating: false,
			pendingBootstrapSessionId: null,
			suppressedReplySessionId: null,
			streamCancelRef: null,
		});
	};

	/** 只关闭 SSE 连接并重置流标记位，保留 messagesMap/messageIds/activeSessionId 等会话数据。 */
	closeSseConnection = () => {
		this.#sessionStream.close();
		this.#set((state) => {
			const retainedIds: string[] = [];
			const retainedMap: Record<string, Message> = {};
			for (const id of state.messageIds) {
				const message = state.messagesMap[id];
				// 中文注释：离开时清掉本地超时报错气泡，避免再进首屏仍看到上一轮客户端超时残留。
				if (!message || isClientReplyTimeoutMessage(message)) continue;
				retainedIds.push(id);
				retainedMap[id] = message;
			}
			return {
				messagesMap: retainedMap,
				messageIds: retainedIds,
				streamingMessageId: null,
				isGenerating: false,
				streamCancelRef: null,
				// 中文注释：离开页面后允许再进时对 responding 做 resume；停留页内的超时抑制仍由发送路径清除。
				suppressedReplySessionId: null,
			};
		});
	};

	/**
	 * 关闭 SSE 连接并清空本地消息数据，保留 activeSessionId 等会话路由状态。
	 * 同时清掉 pendingBootstrap，避免「GlobalEvents 未到 assistant 就离开」后标记残留，
	 * 再进任务详情时被场景 1 守卫挡住、走不了场景 2 hydration。
	 */
	clearLocalMessages = () => {
		this.#sessionStream.close();
		this.#set({
			messagesMap: {},
			messageIds: [],
			streamingMessageId: null,
			isGenerating: false,
			pendingBootstrapSessionId: null,
			suppressedReplySessionId: null,
			streamCancelRef: null,
		});
	};

	/** 更新输入框文本（实现见 Composer）。 */
	setInputText = (text: string) => {
		this.#composer.setInputText(text);
	};

	/** 切换执行模式（实现见 Composer）。 */
	setExecutionMode = (executionMode: ExecutionMode) => {
		this.#composer.setExecutionMode(executionMode);
	};

	/** 清空 composer 草稿（实现见 Composer）。 */
	clearComposerInput = () => {
		this.#composer.clearComposerInput();
	};

	/** 追加 skill 指令（实现见 Composer）。 */
	appendSkillDirective = (skillName: string) => {
		this.#composer.appendSkillDirective(skillName);
	};

	/** 替换 skill 指令前缀（实现见 Composer）。 */
	replaceSkillDirective = (skillName: string) => {
		this.#composer.replaceSkillDirective(skillName);
	};

	/** 添加本地附件（实现见 Composer）。 */
	addAttachment = (file: File) => {
		this.#composer.addAttachment(file);
	};

	/** 上传单个项目文件到 composer（实现见 Composer）。 */
	addUploadedAttachment = async (projectId: string, file: File) => {
		return this.#composer.addUploadedAttachment(projectId, file);
	};

	/** 上传文件夹到 composer（实现见 Composer）。 */
	addUploadedFolderAttachment = async (projectId: string, files: File[]) => {
		return this.#composer.addUploadedFolderAttachment(projectId, files);
	};

	/** 移除 composer 附件（实现见 Composer）。 */
	removeAttachment = (id: string) => {
		this.#composer.removeAttachment(id);
	};

	/** 标记输入框焦点（实现见 Composer）。 */
	setInputFocused = (focused: boolean) => {
		this.#composer.setInputFocused(focused);
	};

	/** 选择模型（实现见 Composer）。 */
	setSelectedModel = (modelId: string) => {
		this.#composer.setSelectedModel(modelId);
	};

	resendMessage = async (messageId: string) => {
		const state = this.#get();
		if (state.isGenerating) return;
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

		const approval = message?.approvals?.find((a) => a.requestId === requestId);
		const assistantId = approval?.assistantId;
		if (!assistantId) {
			console.warn("submitApprovalDecision: missing assistantId, request may fail");
		}

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
				assistant_id: assistantId ?? "",
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

	submitQuestionAnswer = async (messageId: string, requestId: string, answers: string[][]) => {
		const state = this.#get();
		const message = state.messagesMap[messageId];
		const sessionId = message?.conversationId || state.activeSessionId;
		if (!sessionId) return;

		const question = message?.questions?.find((q) => q.requestId === requestId);
		const assistantId = question?.assistantId;
		if (!assistantId) {
			console.warn("submitQuestionAnswer: missing assistantId, request may fail");
		}

		this.#dispatchChat({
			type: "updateQuestionStatus",
			messageId,
			requestId,
			status: "answered",
			answers,
			error: undefined,
		});

		try {
			await sessionApi.submitQuestionAnswer({
				session_id: sessionId,
				request_id: requestId,
				answers,
				assistant_id: assistantId ?? "",
			});
		} catch (err) {
			console.error("submitQuestionAnswer error:", err);
			this.#dispatchChat({
				type: "updateQuestionStatus",
				messageId,
				requestId,
				status: "error",
				answers,
				error: "提交答案失败，请重试",
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
			type: "updateQuestionStatus";
			messageId: string;
			requestId: string;
			status: QuestionRequest["status"];
			answers?: string[][];
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

		case "updateQuestionStatus": {
			const { messageId, requestId, status, answers, error } = action;
			const msg = state.messagesMap[messageId];
			if (!msg?.questions) return state;

			const updatedQuestions = msg.questions.map((question) =>
				question.requestId === requestId
					? {
							...question,
							status,
							answers: answers ?? question.answers,
							error,
						}
					: question,
			);

			return {
				...state,
				messagesMap: {
					...state.messagesMap,
					[messageId]: { ...msg, questions: updatedQuestions },
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
