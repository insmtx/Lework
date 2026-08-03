/**
 * 会话历史加载与「仅进任务」时的 resume/poll 占位。
 *
 * 可以做：分页拉 GetSessionMessages、与本地乐观消息 merge；冷进页且 responding
 * 时插入 resume 占位并开 SessionEvents(replay)；pendingBootstrap 保护；并发去重。
 * 不可以做：在问答等待 GlobalEvents assistant 期间用 responding 去开 SessionEvents
 * （CreateInitialMessage / AddMessage 开流只应由 GlobalEvents assistant 触发）；
 * 不解析 SSE payload。
 */
import { sessionApi } from "../api/sessionApi";
import type { BackendMessage } from "../api/types";
import type { Message } from "../types/chat";
import {
	createAssistantSessionEventsWaitingMessage,
	getSessionLocalMessages,
	isOptimisticMessage,
	mergeSessionMessages,
	normalizedMessageContent,
} from "./messageMerge";
import { attachAssistantReplyTargets, mapBackendMessage } from "./messageReducer";
import type { ChatState } from "./state";

/**
 * HistoryLoader 所需的 store / 开流依赖。
 * 由 ChatActionImpl 注入，避免本模块直接依赖整个 ChatStore。
 */
export type HistoryLoaderDeps = {
	/** 读取当前对话状态 */
	get: () => ChatState;
	/** 部分更新对话状态 */
	set: (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => void;
	/**
	 * 请求打开 SessionEvents。
	 * resume/poll 成功后以 replay=true 调用；不在本模块内联 SSE。
	 */
	startSessionStream: (
		sessionId: string,
		assistantMsgId: string,
		replay?: boolean,
		assistantId?: string,
	) => void | Promise<void>;
};

/**
 * 管理会话历史 hydration，以及进页时 responding / 待回复的 resume·poll 兜底。
 */
export class HistoryLoader {
	readonly #deps: HistoryLoaderDeps;
	/** 同 session 并发 load 去重：后到的调用复用进行中的 Promise。 */
	#loadPromises = new Map<string, Promise<void>>();

	constructor(deps: HistoryLoaderDeps) {
		this.#deps = deps;
	}

	/**
	 * 加载会话历史（对外入口）。
	 * pendingBootstrap 期间直接跳过，避免冲掉新建任务的乐观等待态；
	 * 同一 session 已有进行中的加载则复用该 Promise。
	 */
	load = async (sessionId: string, options?: { resumeStream?: boolean }): Promise<void> => {
		if (this.#deps.get().pendingBootstrapSessionId === sessionId) return;
		const loading = this.#loadPromises.get(sessionId);
		if (loading) return loading;

		const loadPromise = this.#loadInternal(sessionId, options).finally(() => {
			this.#loadPromises.delete(sessionId);
		});
		this.#loadPromises.set(sessionId, loadPromise);
		return loadPromise;
	};

	/**
	 * 分页拉取全部会话消息，直到不足一页或空页。
	 * 与 load 分离，便于 resume=false 的二次回拉复用同一翻页逻辑。
	 */
	#fetchAllConversationMessages = async (sessionId: string): Promise<BackendMessage[]> => {
		const perPage = 100;
		let page = 1;
		let total = Number.POSITIVE_INFINITY;
		const items: BackendMessage[] = [];

		while (items.length < total) {
			const res = await sessionApi.getMessages(sessionId, page, perPage);
			const data = res.data.data;
			const pageItems = data?.items ?? [];
			total = data?.total ?? items.length + pageItems.length;
			items.push(...pageItems);

			// 中文注释：当最后一页不足 perPage 或后端未返回更多记录时，直接停止翻页，避免无意义请求。
			if (pageItems.length < perPage || pageItems.length === 0) {
				break;
			}

			page += 1;
		}

		return items;
	};

	/**
	 * 实际 hydration：GetSession（可选）+ Messages → merge → 按需 resume/poll。
	 * 关键约束见各分支注释；行为须与抽出前 chatSlice 一致。
	 */
	#loadInternal = async (
		sessionId: string,
		options?: { resumeStream?: boolean },
	): Promise<void> => {
		try {
			const shouldCheckRuntime = options?.resumeStream !== false;
			let runtimeStatus: string | undefined;
			let messageCount: number | undefined;
			if (shouldCheckRuntime) {
				try {
					const sessionRes = await sessionApi.get({ session_id: sessionId });
					runtimeStatus = sessionRes.data.data?.runtime_status;
					messageCount = sessionRes.data.data?.message_count;
				} catch (err) {
					console.error("loadConversationMessages get session error:", err);
				}
			}

			const stateBeforeLoad = this.#deps.get();
			const optimisticCountBeforeLoad = getSessionLocalMessages(stateBeforeLoad, sessionId).filter(
				isOptimisticMessage,
			).length;
			const items = await this.#fetchAllConversationMessages(sessionId);

			const persistedMessages = attachAssistantReplyTargets(items.map(mapBackendMessage));
			const state = this.#deps.get();
			if (state.pendingBootstrapSessionId === sessionId) return;
			const localSessionMessages = getSessionLocalMessages(state, sessionId);
			const optimisticCountAfterLoad = localSessionMessages.filter(isOptimisticMessage).length;
			// 如果请求发出后，这个 session 在本地新插入了 optimistic 消息，说明当前返回值已经过时，
			// 直接丢弃，避免把“刚发出去的用户消息”又覆盖没。
			if (optimisticCountAfterLoad > optimisticCountBeforeLoad) return;
			// 生成中或刚完成但本地仍有 optimistic 消息时，优先保留本地消息；
			// 同时过滤掉空内容 assistant 占位，避免在落库后与真实 assistant 重复显示。
			const shouldPreserveLocalMessages =
				state.activeSessionId === sessionId &&
				(state.isGenerating ||
					state.cancellingSessionId === sessionId ||
					optimisticCountAfterLoad > 0);
			const reconcilingLocalMessages = shouldPreserveLocalMessages
				? localSessionMessages.filter(
						(message) =>
							!isOptimisticMessage(message) ||
							message.role !== "assistant" ||
							state.isGenerating ||
							Boolean(normalizedMessageContent(message)),
					)
				: [];
			// 请求返回时若用户已经切到别的 session，则忽略这次结果，避免旧请求反写当前会话。
			if (state.activeSessionId !== sessionId) return;
			const messages = reconcilingLocalMessages.length
				? mergeSessionMessages(persistedMessages, reconcilingLocalMessages)
				: persistedMessages;
			// 中文注释：本地仍有 msg-assistant-waiting-* = 本轮问答在等 GlobalEvents assistant，禁止 resume/poll 开 SE。
			// 不含 resume/poll 占位，避免挡住「仅进任务」的合法回放。
			const awaitingGlobalAssistant = localSessionMessages.some(
				(message) =>
					message.conversationId === sessionId &&
					message.role === "assistant" &&
					message.id.startsWith("msg-assistant-waiting-") &&
					(message.status === "waiting" || message.status === "streaming"),
			);
			const shouldResumeStream =
				runtimeStatus === "responding" &&
				state.cancellingSessionId !== sessionId &&
				state.pendingBootstrapSessionId !== sessionId &&
				!awaitingGlobalAssistant &&
				!(
					state.isGenerating &&
					state.activeSessionId === sessionId &&
					state.streamingMessageId !== null
				);
			// 中文注释：冷进页时末条仍是 user 且尚未 responding，轮询后再决定是否 resume（非问答 GE 路径）。
			const pendingAssistantReply = messages[messages.length - 1]?.role === "user";
			const shouldPoll =
				shouldCheckRuntime &&
				runtimeStatus !== "responding" &&
				pendingAssistantReply &&
				!shouldResumeStream &&
				!awaitingGlobalAssistant &&
				state.pendingBootstrapSessionId !== sessionId;
			const resumeMessage: Message | undefined = shouldResumeStream
				? createAssistantSessionEventsWaitingMessage(
						sessionId,
						`msg-assistant-resume-${Date.now()}`,
					)
				: undefined;
			if (resumeMessage) {
				messages.push(resumeMessage);
			}

			const maps: Record<string, Message> = {};
			const ids: string[] = [];
			for (const m of messages) {
				maps[m.id] = m;
				ids.push(m.id);
			}

			this.#deps.set({
				messagesMap: maps,
				messageIds: ids,
				...(resumeMessage
					? {
							streamingMessageId: resumeMessage.id,
							isGenerating: true,
						}
					: {}),
			});
			if (resumeMessage) {
				void this.#deps.startSessionStream(sessionId, resumeMessage.id, true);
			}

			// workbench 跳转场景：runtime_status 尚未流转，后台轮询等待 responding 后建 SSE 回放
			if (shouldPoll) {
				this.#startPollResume(sessionId, messages.length, messageCount);
			}
		} catch (err) {
			console.error("loadConversationMessages error:", err);
		}
	};

	/**
	 * 插入 poll 占位并后台轮询 runtime_status。
	 * responding → 换成 resume 占位并开 SessionEvents(replay)；
	 * 已出新消息 → 再 load 一次（不做 resume，避免套娃）。
	 */
	#startPollResume = (
		sessionId: string,
		mergedMessageCount: number,
		baselineFromSession?: number,
	) => {
		// 先插入 assistant 占位消息，显示"任务执行中"的 UI 状态
		const pollPlaceholderMsg: Message = {
			id: `msg-assistant-poll-${Date.now()}`,
			conversationId: sessionId,
			role: "assistant",
			content: "",
			timestamp: Date.now(),
		};
		this.#deps.set({
			messagesMap: {
				...this.#deps.get().messagesMap,
				[pollPlaceholderMsg.id]: pollPlaceholderMsg,
			},
			messageIds: [...this.#deps.get().messageIds, pollPlaceholderMsg.id],
			streamingMessageId: pollPlaceholderMsg.id,
			isGenerating: true,
		});
		const baselineMessageCount = baselineFromSession ?? mergedMessageCount;
		void pollRuntimeStatus(sessionId, 60_000, baselineMessageCount).then((pollResult) => {
			if (!pollResult) return;
			const st = this.#deps.get();
			if (st.activeSessionId !== sessionId) return;
			if (pollResult.status === "responding") {
				// 用回放占位替换轮询占位，SSE 回放接管输出
				const resumeMsgId = `msg-assistant-resume-${Date.now()}`;
				const newMap = { ...st.messagesMap };
				delete newMap[pollPlaceholderMsg.id];
				const resumeMsg = createAssistantSessionEventsWaitingMessage(sessionId, resumeMsgId);
				newMap[resumeMsgId] = resumeMsg;
				const newIds = st.messageIds.map((id) => (id === pollPlaceholderMsg.id ? resumeMsgId : id));
				this.#deps.set({
					messagesMap: newMap,
					messageIds: newIds,
					streamingMessageId: resumeMsgId,
					isGenerating: true,
				});
				void this.#deps.startSessionStream(sessionId, resumeMsgId, true);
			} else {
				// 消息已增加但未 responding，重新拉取消息列表同步最新数据
				void this.#loadInternal(sessionId, { resumeStream: false });
			}
		});
	};
}

/**
 * 轮询等待 session 的 runtime_status 变为 "responding"，最长等待 timeoutMs 毫秒。
 * historyLoader 的 poll 占位与任务群聊 assistant 兜底共用，避免两套轮询语义分叉。
 */
export async function pollRuntimeStatus(
	sessionId: string,
	timeoutMs: number,
	baselineMessageCount: number,
): Promise<{ status: string; messageCount?: number } | undefined> {
	const startTime = Date.now();
	const POLL_INTERVAL = 2000;
	while (Date.now() - startTime < timeoutMs) {
		await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL));
		try {
			const res = await sessionApi.get({ session_id: sessionId });
			const status = res.data.data?.runtime_status;
			const messageCount = res.data.data?.message_count;
			if (status === "responding") return { status: "responding", messageCount };
			// 中文注释：以进入轮询时的消息数为基线，避免已有历史的 session 因 messageCount > 1 被误判为已完成。
			if (messageCount !== undefined && messageCount > baselineMessageCount && status === "idle") {
				return { status: "completed", messageCount };
			}
		} catch {
			// 轮询失败继续
		}
	}
	return undefined;
}
