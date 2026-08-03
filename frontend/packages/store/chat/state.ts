/**
 * 对话 store 的状态定义与初始值。
 * 只描述「时间线 + 输入草稿 + 生成锁」等可序列化字段；
 * SSE 连接实例等运行时资源不放这里，由 ChatActionImpl 私有字段持有。
 */
import { mockModelOptions } from "../mocks/chatMocks";
import type { Attachment, ExecutionMode, Message, ModelOption } from "../types/chat";

/**
 * 对话时间线与输入区的前端状态。
 * 连接态（SSE client）不放这里，由 ChatActionImpl 私有字段持有。
 */
export type ChatState = {
	/** 当前时间线消息表：id → Message */
	messagesMap: Record<string, Message>;
	/** 当前时间线消息 id 顺序（渲染顺序） */
	messageIds: string[];
	/** 正在流式写入的 assistant 消息 id；无流时为 null */
	streamingMessageId: string | null;
	/** 是否处于生成中（锁发送、驱动输入区禁用） */
	isGenerating: boolean;
	/** 新建任务跳转保护窗：该 session 的历史加载暂不覆盖乐观消息 */
	pendingBootstrapSessionId: string | null;
	/** 取消中的 session；等 run.cancelled 终态后再收尾 */
	cancellingSessionId: string | null;
	/** 兼容旧字段：取消流式的回调引用（现主要由私有 SSE client 管理） */
	streamCancelRef: (() => void) | null;

	/** 输入框正文草稿 */
	inputText: string;
	/** 输入框待发送附件 */
	inputAttachments: Attachment[];
	/** 输入框是否聚焦 */
	inputFocused: boolean;
	/** 当前选中的模型 id（展示/发送用） */
	selectedModel: string;
	/** 执行模式：default / plan */
	executionMode: ExecutionMode;
	/** 可选模型列表 */
	modelOptions: ModelOption[];
	/** 当前激活的会话 id */
	activeSessionId: string | null;

	/** Token 用量汇总（总量 + 当前会话） */
	tokenUsage: { total: number; currentSession: number };
};

/** 对话 store 的初始空状态。 */
export const initialChatState: ChatState = {
	messagesMap: {},
	messageIds: [],
	streamingMessageId: null,
	isGenerating: false,
	pendingBootstrapSessionId: null,
	cancellingSessionId: null,
	streamCancelRef: null,

	inputText: "",
	inputAttachments: [],
	inputFocused: false,
	selectedModel: "gpt-4",
	executionMode: "default",
	modelOptions: mockModelOptions,
	activeSessionId: null,

	tokenUsage: { total: 0, currentSession: 0 },
};
