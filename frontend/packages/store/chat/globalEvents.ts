/**
 * 进程级 GlobalEvents（长连接 SSE）生命周期与 message.created 处理。
 *
 * 可以做：幂等启动/停止长连接、非当前 session 事件短时缓冲、human 消息 merge、
 * assistant 到达后替换 waiting 占位并请求 SessionEvents。
 * 不可以做：解析 SessionEvents delta 正文（交给 sessionStream + messageReducer）；
 * 不直接拼消息正文。
 */
import { FetchSSEClient } from "@leros/ui/lib/fetch-sse";
import { API_BASE_URL } from "../api/config";
import { sessionApi } from "../api/sessionApi";
import type { BackendWorkTitleUpdatedPayload } from "../api/types";
import type { Message } from "../types/chat";
import { getValidJwtToken } from "../utils/authStorage";
import {
	type BackendGlobalEvent,
	type BackendGlobalMessagePayload,
	createGlobalUserMessageFromEvent,
	getGlobalMessagePayload,
	inheritStreamingAssistantState,
	insertGlobalUserMessageId,
	isGlobalUserEchoMessage,
	isTaskRoomAssistantPlaceholder,
	mergeMessageAttachments,
	parseGlobalEvent,
	parseWorkTitleUpdatedRecord,
} from "./messageMerge";
import { resolveReplyToFromRunId } from "./messageReducer";
import type { ChatState } from "./state";

/** Console 前缀：带 emoji，方便在杂乱日志里一眼认出 GlobalEvents。 */
const GE_LOG = "🌐 [GlobalEvents]";

/**
 * GlobalEvents 所需依赖。
 * startSessionStream / finishStream / loadConversationMessages 由编排层注入，
 * 保证「开流」只走 SessionStream，不在本模块内联 SSE。
 */
export type GlobalEventsDeps = {
	/** 读取当前对话状态 */
	get: () => ChatState;
	/** 部分更新对话状态 */
	set: (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => void;
	/** 读取完整 app store（任务详情 session、标题更新） */
	fullGet: () => Record<string, unknown>;
	/** 新增一条消息 */
	addMessage: (message: Message) => void;
	/** 请求打开 SessionEvents（通常 replay=true） */
	startSessionStream: (
		sessionId: string,
		assistantMsgId: string,
		replay?: boolean,
		assistantId?: string,
	) => void | Promise<void>;
	/** 清除生成态 */
	finishStream: () => void;
	/** 回拉历史（GlobalEvents 晚到且 run 已结束时） */
	loadConversationMessages: (
		sessionId: string,
		options?: { resumeStream?: boolean },
	) => Promise<void>;
};

/**
 * 管理 GlobalEvents 长连接、pending 缓冲，以及 human/assistant message.created。
 */
export class GlobalEventsManager {
	readonly #deps: GlobalEventsDeps;
	#client: FetchSSEClient | null = null;
	#startPromise: Promise<void> | null = null;
	#pendingEvents: BackendGlobalEvent[] = [];

	constructor(deps: GlobalEventsDeps) {
		this.#deps = deps;
	}

	/**
	 * 幂等启动 GlobalEvents 长连接。
	 * Shell / 工作台 / 发送路径可能同时触发；用 startPromise 防止双连接。
	 */
	start = async () => {
		if (this.#client) return;
		if (this.#startPromise) return this.#startPromise;

		this.#startPromise = (async () => {
			try {
				const token = await getValidJwtToken();
				if (!token || this.#client) return;

				const client = new FetchSSEClient(`${API_BASE_URL}/GlobalEvents`, {
					method: "POST",
					headers: { Authorization: `Bearer ${token}` },
					body: {},
					onMessage: (event) => {
						const data = parseGlobalEvent(event.data, event.type);
						if (!data) {
							console.warn(GE_LOG, "unparsed event", event.type, event.data);
							return;
						}
						console.log(GE_LOG, data.type ?? event.type, data);
						this.#handleEvent(data);
					},
					onError: (err) => {
						console.error(GE_LOG, "SSE error:", err);
						this.#client?.close();
						this.#client = null;
					},
				});

				this.#client = client;
				void client.connect();
				console.log(GE_LOG, "connected");
			} catch (err) {
				console.error(GE_LOG, "start error:", err);
			} finally {
				this.#startPromise = null;
			}
		})();

		return this.#startPromise;
	};

	/** 关闭 GlobalEvents 长连接并清空启动 Promise。 */
	stop = () => {
		if (this.#client) {
			console.log(GE_LOG, "disconnected");
		}
		this.#client?.close();
		this.#client = null;
		this.#startPromise = null;
	};

	/**
	 * 判断事件是否属于当前正在看的 session
	 *（activeSessionId 或任务详情页的 activeTaskDetailSessionId）。
	 */
	#isCurrentSession = (event: BackendGlobalEvent) => {
		const sessionId = event.session_id;
		if (!sessionId) return false;
		const state = this.#deps.get();
		if (state.activeSessionId === sessionId) return true;
		const fullState = this.#deps.fullGet() as { activeTaskDetailSessionId?: string | null };
		return fullState.activeTaskDetailSessionId === sessionId;
	};

	/**
	 * 分发单条 GlobalEvents：标题更新直接转 layout；message.created 按当前 session 处理或缓冲。
	 */
	#handleEvent = (event: BackendGlobalEvent) => {
		if (event.type === "work.title.updated") {
			const workTitlePayload = parseWorkTitleUpdatedRecord(
				event.data ?? event.payload,
				event.session_id,
			);
			if (workTitlePayload) {
				const fullState = this.#deps.fullGet() as {
					applyWorkTitleUpdated?: (payload: BackendWorkTitleUpdatedPayload) => void;
				};
				fullState.applyWorkTitleUpdated?.(workTitlePayload);
			}
			return;
		}
		if (event.type !== "message.created") return;
		if (!this.#isCurrentSession(event)) {
			console.log(GE_LOG, "buffer (not current session)", event.session_id);
			this.#bufferPending(event);
			return;
		}
		this.#applyMessageCreated(event);
	};

	/**
	 * 缓冲非当前 session 的 message.created（约 2 分钟、最多 50 条）。
	 * 用于新建任务跳转前 GlobalEvents 已到达的情况。
	 */
	#bufferPending = (event: BackendGlobalEvent) => {
		if (!event.session_id) return;
		const cutoff = Date.now() - 2 * 60_000;
		this.#pendingEvents = [...this.#pendingEvents, event]
			.filter((item) => (item.timestamp ?? Date.now()) >= cutoff)
			.slice(-50);
	};

	/**
	 * 激活 session 后放出缓冲中匹配该 session 的事件。
	 * 由 setActiveSession / bootstrapNewTaskSession 调用。
	 */
	drain = (sessionId: string) => {
		if (!sessionId || !this.#pendingEvents.length) return;
		console.log(
			GE_LOG,
			"drain",
			sessionId,
			this.#pendingEvents.filter((e) => e.session_id === sessionId).length,
		);
		const matched: BackendGlobalEvent[] = [];
		const rest: BackendGlobalEvent[] = [];
		for (const event of this.#pendingEvents) {
			if (event.session_id === sessionId) {
				matched.push(event);
			} else {
				rest.push(event);
			}
		}
		this.#pendingEvents = rest;
		for (const event of matched) {
			this.#applyMessageCreated(event);
		}
	};

	/** 按 sender_type 分流 human / assistant 的 message.created。 */
	#applyMessageCreated = (event: BackendGlobalEvent) => {
		const payload = getGlobalMessagePayload(event);
		if (payload.sender_type === "human") {
			this.#mergeUserMessage(event, payload);
			return;
		}
		if (payload.sender_type === "assistant") {
			this.#startAssistantResponse(event, payload);
		}
	};

	/**
	 * 合并 GlobalEvents human 消息到时间线：替换乐观 user，或插入队友消息。
	 * 插入位置锚定在本轮 waiting/streaming assistant 之前。
	 */
	#mergeUserMessage = (event: BackendGlobalEvent, payload: BackendGlobalMessagePayload) => {
		const sessionId = event.session_id;
		if (!sessionId) return;
		const incoming = createGlobalUserMessageFromEvent(event, payload);
		if (!incoming) return;

		this.#deps.set((state) => {
			const existingId =
				state.messageIds.find((id) => id === incoming.id) ??
				state.messageIds.find((id) => {
					const message = state.messagesMap[id];
					return (
						message?.conversationId === sessionId &&
						message.sequence !== undefined &&
						message.sequence === payload.sequence
					);
				}) ??
				[...state.messageIds]
					.reverse()
					.find((id) => isGlobalUserEchoMessage(state.messagesMap[id], incoming));

			if (!existingId) {
				const nextMap = { ...state.messagesMap, [incoming.id]: incoming };
				return {
					messagesMap: nextMap,
					messageIds: insertGlobalUserMessageId(
						state.messageIds,
						nextMap,
						incoming,
						state.streamingMessageId,
					),
				};
			}

			const nextMap = { ...state.messagesMap };
			const current = nextMap[existingId];
			delete nextMap[existingId];
			nextMap[incoming.id] = {
				...current,
				...incoming,
				attachments:
					mergeMessageAttachments(incoming.attachments, current?.attachments) ??
					incoming.attachments ??
					current?.attachments,
				// 实时回推可能带有落库后的展示 metadata，本地没有时不能把它覆盖丢。
				metadata: current?.metadata ?? incoming.metadata,
			};
			return {
				messagesMap: nextMap,
				messageIds: state.messageIds.map((id) => (id === existingId ? incoming.id : id)),
			};
		});
	};

	/**
	 * GlobalEvents assistant 到达：替换 waiting 占位、写入 runId，再按 runtime 决定开 SessionEvents 或拉历史。
	 */
	#startAssistantResponse = (event: BackendGlobalEvent, payload: BackendGlobalMessagePayload) => {
		const sessionId = event.session_id;
		const runId = payload.run_id;
		if (!sessionId || !runId) return;

		const responseMessageId = `msg-assistant-${runId}`;
		const state = this.#deps.get();
		// 取消命令发送后，GlobalEvents 中可能仍有本轮迟到的 assistant started。
		// 不能为已取消的 run 再创建「AI 员工已接单」占位或重新建立 SSE。
		if (state.cancellingSessionId === sessionId) return;
		if (state.messagesMap[responseMessageId]) return;
		const currentStreamingMessage = state.streamingMessageId
			? state.messagesMap[state.streamingMessageId]
			: undefined;
		// 仅当当前流式消息已绑定本轮 runId 时跳过，避免重复处理同一条 GlobalEvents。
		if (
			currentStreamingMessage?.conversationId === sessionId &&
			currentStreamingMessage.runId === runId
		) {
			return;
		}
		const waitingPlaceholderId = [...state.messageIds]
			.reverse()
			.find((id) => isTaskRoomAssistantPlaceholder(state.messagesMap[id], sessionId));

		const assistantMsg: Message = {
			id: responseMessageId,
			conversationId: sessionId,
			runId,
			role: "assistant",
			content: "",
			timestamp: event.timestamp || Date.now(),
			status: "streaming",
			statusText: "AI 员工已接单，正在生成回复...",
			// 后端当前用 req_用户消息ID 作为 run_id，前端据此展示本轮 AI 回复对应的问题。
			replyTo: resolveReplyToFromRunId(runId, state.messagesMap, sessionId),
			author: {
				id: payload.assistant_id !== undefined ? String(payload.assistant_id) : runId,
				name: payload.assistant_name || payload.sender_name || "Lework",
				type: "assistant",
			},
		};

		if (waitingPlaceholderId) {
			this.#deps.set((currentState) => {
				const placeholder = currentState.messagesMap[waitingPlaceholderId];
				const nextMap = { ...currentState.messagesMap };
				delete nextMap[waitingPlaceholderId];
				nextMap[assistantMsg.id] = inheritStreamingAssistantState(assistantMsg, placeholder);
				return {
					messagesMap: nextMap,
					messageIds: currentState.messageIds.map((id) =>
						id === waitingPlaceholderId ? assistantMsg.id : id,
					),
				};
			});
		} else {
			this.#deps.addMessage(assistantMsg);
		}
		this.#deps.set({
			streamingMessageId: assistantMsg.id,
			isGenerating: true,
			activeSessionId: sessionId,
			pendingBootstrapSessionId: null,
		});
		void this.#openStreamAfterAssistant(sessionId, assistantMsg, payload.assistant_id);
	};

	/**
	 * assistant 落位后：若仍 responding 则开 SessionEvents(replay)；否则拉历史并结束 generating。
	 */
	#openStreamAfterAssistant = async (
		sessionId: string,
		assistantMsg: Message,
		assistantId?: string,
	) => {
		try {
			const sessionRes = await sessionApi.get({ session_id: sessionId });
			const runtimeStatus = sessionRes.data.data?.runtime_status;
			if (runtimeStatus !== "responding") {
				// GlobalEvents 晚到时 run 可能已结束，直接从 DB 拉最新消息，避免 replay 空挂。
				await this.#deps.loadConversationMessages(sessionId, { resumeStream: false });
				this.#deps.finishStream();
				return;
			}
		} catch (err) {
			console.error("startAssistantStreamAfterGlobalEvent get session error:", err);
		}
		// run 仍在进行，由 GlobalEvents 触发 SessionEvents 回放本轮回复事件。
		void this.#deps.startSessionStream(sessionId, assistantMsg.id, true, assistantId);
	};
}
