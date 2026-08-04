/**
 * 发送管道共享依赖类型。
 *
 * 由 ChatActionImpl 注入；各 send/* 模块只通过本类型访问 store / 开流 / 副作用，
 * 不直接依赖整个 ChatStore 或 layoutSlice。
 */
import type { Message } from "../../types/chat";
import type { ChatEffects } from "../effects";
import type { ChatState } from "../state";

/**
 * 发送管道所需依赖。
 */
export type SendPipelineDeps = {
	/** 读取当前对话状态 */
	get: () => ChatState;
	/** 部分更新对话状态（含 composer 清空等 ChatState 字段） */
	set: (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => void;
	/**
	 * 写入跨 slice 字段（currentView 等）。
	 * 优先走 effects；少量直接 set 仅用于兼容 Zustand 联合 store。
	 */
	setStore: (partial: Record<string, unknown>) => void;
	/** 读取完整 app store */
	fullGet: () => Record<string, unknown>;
	/** 追加一条消息 */
	addMessage: (message: Message) => void;
	/** 更新一条消息 */
	updateMessage: (id: string, value: Message) => void;
	/** 幂等启动 GlobalEvents */
	startGlobalEvents: () => Promise<void>;
	/** 排空指定 session 的 GlobalEvents 缓冲 */
	drainGlobalEvents: (sessionId: string) => void;
	/** 打开 SessionEvents */
	startSessionStream: (
		sessionId: string,
		assistantMsgId: string,
		replay?: boolean,
		assistantId?: string,
	) => void | Promise<void>;
	/** 清除生成态 */
	finishStream: () => void;
	/** 回拉历史（任务群聊 fallback completed 时用） */
	loadConversationMessages: (
		sessionId: string,
		options?: { resumeStream?: boolean },
	) => Promise<void>;
	/** layout / project 副作用出口 */
	effects: ChatEffects;
};
