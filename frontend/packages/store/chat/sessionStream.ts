/**
 * 单会话 SessionEvents（短连接 SSE）生命周期管理。
 *
 * 可以做：建立/关闭 SessionEvents、idle 超时兜底、把事件应用到 streaming assistant、
 * run 终态后 finish 并触发历史回拉。
 * 不可以做：决定「该不该发消息」、解析 GlobalEvents、改 layout 导航；
 * 对 layout/project 的副作用通过 deps 回调交给编排层。
 */
import { FetchSSEClient } from "@leros/ui/lib/fetch-sse";
import { API_BASE_URL } from "../api/config";
import type { BackendWorkTitleUpdatedPayload, SSEMessageEvent } from "../api/types";
import type { Message } from "../types/chat";
import { getValidJwtToken } from "../utils/authStorage";
import { parseWorkTitleUpdatedPayload, SESSION_EVENTS_IDLE_FALLBACK_MS } from "./messageMerge";
import { applySessionEventToMessage } from "./messageReducer";
import type { ChatState } from "./state";

/**
 * SessionStream 所需的 store / 副作用依赖。
 * 由 ChatActionImpl 注入，避免本模块直接依赖整个 ChatStore。
 */
export type SessionStreamDeps = {
	/** 读取当前对话状态 */
	get: () => ChatState;
	/** 部分更新对话状态 */
	set: (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => void;
	/** 更新单条消息（走 chatReducer） */
	updateMessage: (id: string, value: Message) => void;
	/** 流结束后回拉历史（不做 resume） */
	loadConversationMessages: (
		sessionId: string,
		options?: { resumeStream?: boolean },
	) => Promise<void>;
	/** 读取完整 app store（标题更新、刷新项目详情） */
	fullGet: () => Record<string, unknown>;
	/** run 终态后按 session 定位并刷新项目详情（可选） */
	refreshProjectForSession?: (sessionId: string) => void;
};

/**
 * 管理一条 SessionEvents 连接及其与 ChatState 生成态的绑定。
 */
export class SessionStream {
	readonly #deps: SessionStreamDeps;
	#client: FetchSSEClient | null = null;
	#sessionId: string | null = null;
	#assistantMsgId: string | null = null;
	#idleFallbackTimer: ReturnType<typeof setTimeout> | null = null;

	constructor(deps: SessionStreamDeps) {
		this.#deps = deps;
	}

	/** 当前绑定的 sessionId；无连接时为 null。 */
	get boundSessionId(): string | null {
		return this.#sessionId;
	}

	/** 当前是否持有活跃的 SSE client。 */
	get isOpen(): boolean {
		return this.#client !== null;
	}

	/** 清除 idle 兜底定时器。 */
	#clearIdleFallback = () => {
		if (this.#idleFallbackTimer) {
			clearTimeout(this.#idleFallbackTimer);
			this.#idleFallbackTimer = null;
		}
	};

	/** 清空 session/assistant 绑定与 idle 定时器（不关 client）。 */
	#resetBinding = () => {
		this.#clearIdleFallback();
		this.#sessionId = null;
		this.#assistantMsgId = null;
	};

	/**
	 * 打开连接后启动 idle 计时：长时间无事件则拉历史收尾，避免 UI 永久 generating。
	 */
	#scheduleIdleFallback = (sessionId: string) => {
		this.#clearIdleFallback();
		this.#idleFallbackTimer = setTimeout(() => {
			const state = this.#deps.get();
			if (this.#sessionId !== sessionId || !state.isGenerating) return;
			void this.#recoverStaleStream(sessionId);
		}, SESSION_EVENTS_IDLE_FALLBACK_MS);
	};

	/**
	 * idle 超时后关流、回拉历史并 finish。
	 * 用于后端已落库但前端未收到终态事件的情况。
	 */
	#recoverStaleStream = async (sessionId: string) => {
		this.#resetBinding();
		if (this.#client) {
			this.#client.close();
			this.#client = null;
		}
		try {
			await this.#deps.loadConversationMessages(sessionId, { resumeStream: false });
		} catch (err) {
			console.error("recoverStaleSessionStream error:", err);
		} finally {
			this.finish();
		}
	};

	/**
	 * 建立 SessionEvents 短连接，把事件写入 assistantMsgId 对应气泡。
	 * replay=true 用于任务场景（GlobalEvents 之后补流）；纯 chat 发完立刻开流时为 false。
	 */
	start = async (
		sessionId: string,
		assistantMsgId: string,
		replay = false,
		assistantId?: string,
	) => {
		if (this.#client) {
			this.#client.close();
			this.#client = null;
		}
		this.#resetBinding();
		this.#sessionId = sessionId;
		this.#assistantMsgId = assistantMsgId;

		const url = `${API_BASE_URL}/SessionEvents`;
		const token = await getValidJwtToken();
		if (!token) {
			this.finish();
			return;
		}

		const client = new FetchSSEClient(url, {
			method: "POST",
			headers: { Authorization: `Bearer ${token}` },
			body: {
				session_id: sessionId,
				...(replay ? { replay: true } : {}),
				...(assistantId !== undefined ? { assistant_id: assistantId } : {}),
			},
			onOpen: () => {
				this.#scheduleIdleFallback(sessionId);
			},
			onMessage: (event) => {
				this.#clearIdleFallback();
				try {
					const data = JSON.parse(event.data) as SSEMessageEvent;
					const eventType = event.type ?? data.type;

					if (eventType === "work.title.updated") {
						const workTitlePayload = parseWorkTitleUpdatedPayload(data);
						if (workTitlePayload) {
							const fullState = this.#deps.fullGet() as {
								applyWorkTitleUpdated?: (payload: BackendWorkTitleUpdatedPayload) => void;
							};
							fullState.applyWorkTitleUpdated?.(workTitlePayload);
						}
						return;
					}

					const targetAssistantMsgId = this.#assistantMsgId ?? assistantMsgId;
					const msg = this.#deps.get().messagesMap[targetAssistantMsgId];
					if (msg) {
						const nextMsg = applySessionEventToMessage(msg, data, eventType, {
							appendContent: true,
						});
						if (nextMsg !== msg) {
							this.#deps.updateMessage(targetAssistantMsgId, nextMsg);
						}
					}

					if (
						eventType === "run.completed" ||
						eventType === "run.failed" ||
						eventType === "run.cancelled"
					) {
						this.#resetBinding();
						this.finish();
						this.#client?.close();
						this.#client = null;
						// 清除取消标记
						this.#deps.set({ cancellingSessionId: null });
						// 会话结束后回拉历史，确保持久化 usage 能立即参与页面汇总展示。
						void this.#deps.loadConversationMessages(sessionId, {
							resumeStream: false,
						});
						this.#deps.refreshProjectForSession?.(sessionId);
					}
				} catch (err) {
					// 正文只接受 run.completed 的最终结果，解析失败的流片段不再兜底写入正文。
					console.error("SSE message parse error:", err);
				}
			},
			onError: (err) => {
				console.error("SSE error:", err);
				this.#resetBinding();
				this.finish();
			},
		});

		this.#deps.set({ streamCancelRef: () => client.close() });
		void client.connect();
		this.#client = client;
	};

	/**
	 * 清除生成态（streamingMessageId / isGenerating / streamCancelRef）。
	 * 不强制关闭 client；终态路径会先 close 再 finish。
	 */
	finish = () => {
		this.#clearIdleFallback();
		this.#deps.set({
			streamingMessageId: null,
			isGenerating: false,
			streamCancelRef: null,
		});
	};

	/**
	 * 关闭当前 SessionEvents 连接并重置绑定。
	 * 用于切会话、清空本地消息、离开页面等。
	 */
	close = () => {
		this.#clearIdleFallback();
		if (this.#client) {
			this.#client.close();
			this.#client = null;
		}
		this.#resetBinding();
	};

	/**
	 * 切到新 session 时：若当前流绑定的不是目标 session，则关流并重置绑定。
	 * 返回是否关闭了连接。
	 */
	closeIfBoundToOtherSession = (sessionId: string): boolean => {
		if (this.#client && this.#sessionId !== sessionId) {
			this.close();
			return true;
		}
		return false;
	};
}
