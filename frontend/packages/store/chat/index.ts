/**
 * `store/chat` 公共出口。
 * 对外统一从这里 re-export 状态、纯函数与类型，避免业务方直接依赖内部文件路径。
 * SessionStream / GlobalEvents / HistoryLoader / Composer / send 实现类由 chatSlice
 * 内部装配，不从此处导出，以免 UI 绕过编排直接开流或改草稿。
 */

export type {
	BackendGlobalEvent,
	BackendGlobalMessagePayload,
} from "./messageMerge";
export {
	ASSISTANT_GLOBAL_EVENTS_TIMEOUT_TEXT,
	ASSISTANT_SESSION_EVENTS_TIMEOUT_TEXT,
	ASSISTANT_SESSION_EVENTS_WAITING_TEXT,
	allLocalMessagesBelongToSession,
	createAssistantSessionEventsWaitingMessage,
	createGlobalUserMessageFromEvent,
	getGlobalMessagePayload,
	getSessionLocalMessages,
	inheritStreamingAssistantState,
	insertGlobalUserMessageId,
	isClientReplyTimeoutMessage,
	isGlobalUserEchoMessage,
	isOptimisticMessage,
	isTaskRoomAssistantPlaceholder,
	mergeMessageAttachments,
	mergeSessionMessages,
	normalizedMessageContent,
	parseGlobalEvent,
	parseWorkTitleUpdatedPayload,
	parseWorkTitleUpdatedRecord,
	resolveSessionEventsWaitingContext,
	retainLocalMessagesForSession,
	SESSION_EVENTS_CONNECT_FALLBACK_MS,
	SESSION_EVENTS_IDLE_FALLBACK_MS,
	TASK_ROOM_ASSISTANT_START_FALLBACK_MS,
} from "./messageMerge";

export {
	applySessionEventsToMessage,
	applySessionEventToMessage,
	attachAssistantReplyTargets,
	buildReplyToFromMessage,
	getApprovalStatus,
	mapBackendAttachment,
	mapBackendMessage,
	resolveActiveRunIdForCancel,
	resolveReplyToFromRunId,
} from "./messageReducer";
export type { ChatState } from "./state";
export { initialChatState } from "./state";
