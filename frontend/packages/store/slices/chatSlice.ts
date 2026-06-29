import { FetchSSEClient } from "@leros/ui/lib/fetch-sse";
import { API_BASE_URL } from "../api/config";
import { projectFileApi } from "../api/projectFileApi";
import { sessionApi } from "../api/sessionApi";
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
  BackendWorkTitleUpdatedPayload,
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
  MessageAttachment,
  MessageMetadata,
  MessageProcessStep,
  MessageRole,
  MessageUsage,
  ModelOption,
  QuestionItem,
  QuestionRequest,
  RuntimeTodoItem,
  TodoStatus,
  ToolCall,
  ToolCallStatus,
} from "../types/chat";
import { flattenActions } from "../utils";
import { getValidJwtToken } from "../utils/authStorage";
import { formatFileSize, parseOptionalTimestamp } from "../utils/format";
import {
  buildMessageMetadata,
  enrichAssistantMessageMetrics,
  latencyFromRunCompletedTimes,
} from "../utils/messageMetrics";

export type ChatState = {
  messagesMap: Record<string, Message>;
  messageIds: string[];
  streamingMessageId: string | null;
  isGenerating: boolean;
  pendingBootstrapSessionId: string | null;
  cancellingSessionId: string | null;
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
  pendingBootstrapSessionId: null,
  cancellingSessionId: null,
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
  partial:
    | ChatStore
    | Partial<ChatStore>
    | ((state: ChatStore) => ChatStore | Partial<ChatStore>),
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
    metadata: buildMessageMetadata(msg.metadata),
    usage: mapUsage(msg.usage),
  };

  let mapped = applySessionEventsToMessage(message, msg.chunks, {
    appendContent: !message.content,
    finalContent: message.content,
  });
  if (msg.artifacts?.length) {
    const artifacts = msg.artifacts
      .map(mapArtifactPayload)
      .filter(
        (artifact): artifact is MessageArtifact => artifact !== undefined,
      );
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
      .filter(
        (attachment): attachment is MessageAttachment =>
          attachment !== undefined,
      );
    if (attachments.length) {
      mapped = { ...mapped, attachments };
    }
  }
  return enrichAssistantMessageMetrics(mapped);
}

function mapBackendAttachment(
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
    createdAt: messageCreatedAt,
    url:
      attachment.PublicURL?.trim() ||
      attachment.public_url?.trim() ||
      undefined,
  };
}

function mapComposerAttachment(
  attachment: Attachment,
): MessageAttachment | undefined {
  const fileUploadId = attachment.fileUploadId?.trim();
  if (!fileUploadId) return undefined;

  return {
    id: attachment.id,
    fileUploadId,
    name: attachment.name,
    mimeType:
      attachment.mimeType ||
      attachment.file?.type ||
      "application/octet-stream",
    size: attachment.size,
    createdAt: Date.now(),
    url: attachment.url,
    storageUri: attachment.storageUri,
  };
}

function mapComposerAttachments(
  attachments?: Attachment[],
): MessageAttachment[] | undefined {
  const mapped = attachments
    ?.map(mapComposerAttachment)
    .filter(
      (attachment): attachment is MessageAttachment => attachment !== undefined,
    );
  return mapped?.length ? mapped : undefined;
}

function mapOutgoingAttachments(
  attachments?: Attachment[],
):
  | Array<{ file_upload_id: string; name: string; mime_type: string; size: number }>
  | undefined {
  const mapped = attachments
    ?.filter(
      (attachment): attachment is Attachment & { fileUploadId: string } =>
        Boolean(attachment.fileUploadId?.trim()),
    )
    .map((attachment) => ({
      file_upload_id: attachment.fileUploadId.trim(),
      name: attachment.name,
      mime_type:
        attachment.mimeType ||
        attachment.file?.type ||
        "application/octet-stream",
      // 中文注释：上传接口已经返回真实大小，随消息落库后历史会话才能展示准确文件大小。
      size: attachment.size,
    }));
  return mapped?.length ? mapped : undefined;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeSkillDirectiveName(skillName: string): string {
  return skillName.trim().replace(/^\/+/, "");
}

function appendSkillDirectiveToInput(
  inputText: string,
  skillName: string,
): string {
  const normalizedName = normalizeSkillDirectiveName(skillName);
  if (!normalizedName) return inputText;

  const token = `/${normalizedName}`;
  const tokenPattern = new RegExp(`(^|\\s)${escapeRegExp(token)}(?=\\s|$)`);
  const trimmed = inputText.trimStart();
  if (tokenPattern.test(trimmed)) {
    return trimmed.endsWith(" ") ? trimmed : `${trimmed} `;
  }

  const directivePrefixMatch = trimmed.match(/^((?:\/[^\s/]+\s+)*)/);
  const directivePrefix = directivePrefixMatch?.[0] ?? "";
  const rest = trimmed.slice(directivePrefix.length);
  const nextDirectivePrefix = directivePrefix
    ? `${directivePrefix.trimEnd()} ${token} `
    : `${token} `;

  return `${nextDirectivePrefix}${rest}`;
}

function replaceSkillDirectiveInInput(
  inputText: string,
  skillName: string,
): string {
  const normalizedName = normalizeSkillDirectiveName(skillName);
  if (!normalizedName) return inputText;

  const token = `/${normalizedName}`;
  const trimmed = inputText.trimStart();
  const rest = trimmed.replace(/^(?:\/[^\s/]+\s+)*/, "");
  return rest ? `${token} ${rest}` : `${token} `;
}

function revokeLocalAttachmentUrls(attachments: Attachment[]) {
  for (const attachment of attachments) {
    if (attachment.url?.startsWith("blob:")) {
      URL.revokeObjectURL(attachment.url);
    }
  }
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

type NormalizedSessionEvent =
  | Exclude<BackendMessageChunk, string>
  | SSEMessageEvent;
type SessionEventLike = BackendMessageChunk | SSEMessageEvent;

function mapUsage(usage?: {
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
  return {
    inputTokens: usage.input_tokens,
    outputTokens: usage.output_tokens,
    totalTokens: usage.total_tokens,
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

function completeTodos(
  todos: RuntimeTodoItem[] | undefined,
): RuntimeTodoItem[] | undefined {
  if (!todos?.length || todos.every((todo) => todo.status === "completed")) {
    return todos;
  }
  return todos.map((todo) =>
    todo.status === "completed" ? todo : { ...todo, status: "completed" },
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function normalizeSessionEvent(
  event: SessionEventLike,
): NormalizedSessionEvent | undefined {
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

function getEventPayload(
  event: NormalizedSessionEvent,
): BackendSessionEventPayload {
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

function getRunResultMessage(
  payload: BackendSessionEventPayload,
): string | undefined {
  if (typeof payload.message === "string" && payload.message.trim()) {
    return payload.message;
  }
  if (!payload.result || typeof payload.result !== "object") return undefined;
  const value = payload.result as { message?: unknown };
  return typeof value.message === "string" ? value.message : undefined;
}

function getRunFailedMessage(
  payload: BackendSessionEventPayload,
): string | undefined {
  if (typeof payload.error === "string" && payload.error.trim()) {
    return payload.error;
  }
  return getRunResultMessage(payload);
}

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

function createThinkingStepsFromContent(
  content: string,
  startIndex: number,
): MessageProcessStep[] {
  const thinkingSegments = splitThinkingStepContent(content);
  return thinkingSegments.map((segment, index) => ({
    id: `thinking-${startIndex + index + 1}`,
    type: "thinking" as const,
    content: segment,
  }));
}

function rebuildTrailingThinkingSteps(
  steps: MessageProcessStep[] | undefined,
  content: string,
): MessageProcessStep[] {
  const stableSteps = [...(steps ?? [])];
  return [
    ...stableSteps,
    ...createThinkingStepsFromContent(content, stableSteps.length),
  ];
}

function appendProcessThinkingStep(
  steps: MessageProcessStep[] | undefined,
  delta: string,
): MessageProcessStep[] {
  if (!delta) return steps ?? [];

  const next = [...(steps ?? [])];
  const lastStep = next.at(-1);
  if (lastStep?.type === "thinking") {
    return rebuildTrailingThinkingSteps(
      next.slice(0, -1),
      lastStep.content + delta,
    );
  }

  return rebuildTrailingThinkingSteps(next, delta);
}

function appendProcessToolCallStep(
  steps: MessageProcessStep[] | undefined,
  toolCallId: string,
): MessageProcessStep[] {
  if (!toolCallId) return steps ?? [];
  if (
    steps?.some(
      (step) => step.type === "tool_call" && step.toolCallId === toolCallId,
    )
  ) {
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

type ApplySessionEventOptions = {
  appendContent: boolean;
  finalContent?: string;
};

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

function metadataFromPayload(
  payload: BackendSessionEventPayload,
): MessageMetadata | undefined {
  const usage = mapUsage(payload.usage ?? payload);
  const streamLatency = latencyFromRunCompletedTimes(
    payload.started_at,
    payload.completed_at,
  );
  return buildMessageMetadata(
    {
      ...payload.metadata,
      model: payload.metadata?.model ?? payload.model,
      latency: payload.metadata?.latency ?? streamLatency,
    },
    usage,
  );
}

function mergeToolCalls(
  current: ToolCall[] | undefined,
  updates: ToolCall[],
): ToolCall[] {
  return updates.reduce(
    (acc, update) => upsertToolCall(acc, update),
    current ?? [],
  );
}

function getTodoItemsFromValue(
  value: unknown,
): BackendRuntimeTodoItem[] | undefined {
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

function mapArtifactPayload(
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
):
  | Pick<ApprovalRequest, "requestId" | "status" | "action" | "reason">
  | undefined {
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
  const index = list.findIndex(
    (approval) => approval.requestId === update.requestId,
  );
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
  const index = list.findIndex(
    (approval) => approval.requestId === decision.requestId,
  );
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

function getQuestionRequestPayload(
  payload: BackendSessionEventPayload,
): BackendQuestionRequestPayload | undefined {
  if (payload.question_request) return payload.question_request;
  if (payload.request_id && payload.questions)
    return payload as BackendQuestionRequestPayload;
  return undefined;
}

function getQuestionAnswerPayload(
  payload: BackendSessionEventPayload,
): BackendQuestionAnswerPayload | undefined {
  if (payload.question_answer) return payload.question_answer;
  if (payload.request_id && payload.answers)
    return payload as BackendQuestionAnswerPayload;
  return undefined;
}

function mapQuestionRequestPayload(
  payload: BackendQuestionRequestPayload,
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
    toolCallId: payload.tool_call_id?.trim() || undefined,
    messageId: payload.message_id?.trim() || undefined,
    metadata: payload.metadata,
    status: "pending",
  };
}

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

function upsertToolCall(
  current: ToolCall[] | undefined,
  update: ToolCall,
): ToolCall[] {
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
      ? normalizeToolCallStatus(
          payload.status ?? (payload.is_error ? "error" : "success"),
        )
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
        ("tool_calls" in normalizedEvent
          ? normalizedEvent.tool_calls
          : undefined),
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
        ? mapApprovalRequestPayload(approvalPayload)
        : undefined;
      if (!approval) return message;
      return {
        ...message,
        approvals: mergeApprovalRequest(message.approvals, approval),
      };
    }
    case "approval.resolved": {
      const decisionPayload = getApprovalDecisionPayload(payload);
      const decision = decisionPayload
        ? mapApprovalDecisionPayload(decisionPayload)
        : undefined;
      if (!decision) return message;
      return {
        ...message,
        approvals: mergeApprovalDecision(message.approvals, decision),
      };
    }
    case "question.asked": {
      const questionPayload = getQuestionRequestPayload(payload);
      const question = questionPayload
        ? mapQuestionRequestPayload(questionPayload)
        : undefined;
      if (!question) return message;
      return {
        ...message,
        questions: mergeQuestionRequest(message.questions, question),
      };
    }
    case "question.answered": {
      const answerPayload = getQuestionAnswerPayload(payload);
      const answer = answerPayload
        ? mapQuestionAnswerPayload(answerPayload)
        : undefined;
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

      return {
        ...message,
        processSteps: appendProcessThinkingStep(message.processSteps, content),
      };
    }
    case "reasoning.delta": {
      const thinking =
        payload.thinking ?? getEventContent(normalizedEvent, payload);
      if (!thinking) return message;
      return {
        ...message,
        processSteps: appendProcessThinkingStep(message.processSteps, thinking),
      };
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
      return {
        ...message,
        toolCalls: upsertToolCall(message.toolCalls, toolCall),
        processSteps: appendProcessToolCallStep(
          message.processSteps,
          toolCall.id,
        ),
      };
    }
    case "run.completed": {
      const resultMessage = getRunResultMessage(payload);
      const metadata = metadataFromPayload(payload);
      const usage = mapUsage(payload.usage ?? payload);
      const artifacts = payload.artifacts
        ?.map(mapArtifactPayload)
        .filter(
          (artifact): artifact is MessageArtifact => artifact !== undefined,
        );
      return enrichAssistantMessageMetrics({
        ...message,
        content:
          options.appendContent && resultMessage
            ? resultMessage
            : message.content,
        processSteps: pruneFinalContentProcessSteps(
          message.processSteps,
          resultMessage,
        ),
        todos: completeTodos(message.todos),
        artifacts: artifacts?.length
          ? mergeArtifacts(message.artifacts, artifacts)
          : message.artifacts,
        metadata: metadata
          ? { ...message.metadata, ...metadata }
          : message.metadata,
        usage: usage ?? message.usage,
      });
    }
    case "run.failed": {
      const failedMessage = getRunFailedMessage(payload);
      if (!failedMessage) return message;
      return {
        ...message,
        // 中文注释：失败事件也要回填到当前 assistant 消息里，避免界面只剩空占位。
        content: failedMessage,
      };
    }
    default:
      return message;
  }
}

function applySessionEventsToMessage(
  message: Message,
  events: BackendMessageChunk[] | undefined,
  options: ApplySessionEventOptions,
): Message {
  if (!events?.length) return message;
  return events.reduce(
    (current, event) =>
      applySessionEventToMessage(current, event, undefined, options),
    message,
  );
}

function isOptimisticMessage(message: Message): boolean {
  return (
    message.id.startsWith("msg-user-") ||
    message.id.startsWith("msg-assistant-")
  );
}

function normalizedMessageContent(message: Message): string {
  return message.content.trim().replace(/\s+/g, " ");
}

function messageMergeKey(message: Message): string | undefined {
  const content = normalizedMessageContent(message);
  if (!content) return undefined;
  return `${message.role}:${content}`;
}

function countMatchingMessages(
  messages: Message[],
  target: Message,
  targetIndex?: number,
): number {
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

function countMessagesByRole(
  messages: Message[],
  role: MessageRole,
  targetIndex?: number,
): number {
  let count = 0;
  const end = targetIndex ?? messages.length - 1;
  for (let index = 0; index <= end; index += 1) {
    if (messages[index]?.role === role) {
      count += 1;
    }
  }
  return count;
}

function findMessageByRoleOccurrence(
  messages: Message[],
  role: MessageRole,
  occurrence: number,
): Message | undefined {
  if (occurrence <= 0) return undefined;

  let count = 0;
  for (const message of messages) {
    if (message.role !== role) continue;
    count += 1;
    if (count === occurrence) {
      return message;
    }
  }
  return undefined;
}

function findPersistedMessageMatch(
  persistedMessages: Message[],
  localMessages: Message[],
  localMessage: Message,
  localIndex?: number,
): Message | undefined {
  const exactMatch = persistedMessages.find(
    (message) => message.id === localMessage.id,
  );
  if (exactMatch) return exactMatch;

  if (!isOptimisticMessage(localMessage)) return undefined;

  // 中文注释：流式阶段的 optimistic 消息和落库消息 id 不同，这里按同角色出现顺序兜底配对，
  // 避免 markdown/空白字符略有差异时把同一轮回复渲染成两条。
  const roleOccurrence = countMessagesByRole(
    localMessages,
    localMessage.role,
    localIndex,
  );
  return findMessageByRoleOccurrence(
    persistedMessages,
    localMessage.role,
    roleOccurrence,
  );
}

function shouldKeepLocalMessage(
  persistedMessages: Message[],
  localMessages: Message[],
  localMessage: Message,
  localIndex: number,
): boolean {
  if (
    findPersistedMessageMatch(
      persistedMessages,
      localMessages,
      localMessage,
      localIndex,
    )
  ) {
    return false;
  }
  if (!isOptimisticMessage(localMessage)) return true;
  if (!messageMergeKey(localMessage)) return true;

  const localOccurrence = countMatchingMessages(
    localMessages,
    localMessage,
    localIndex,
  );
  const persistedOccurrence = countMatchingMessages(
    persistedMessages,
    localMessage,
  );
  return persistedOccurrence < localOccurrence;
}

function compareMessages(a: Message, b: Message): number {
  if (a.sequence !== undefined && b.sequence !== undefined) {
    return a.sequence - b.sequence;
  }
  return a.timestamp - b.timestamp;
}

function mergeMessageAttachments(
  persistedAttachments: MessageAttachment[] | undefined,
  localAttachments: MessageAttachment[] | undefined,
): MessageAttachment[] | undefined {
  if (!persistedAttachments?.length) return persistedAttachments;
  if (!localAttachments?.length) return persistedAttachments;

  const localByUploadId = new Map(
    localAttachments.map(
      (attachment) => [attachment.fileUploadId, attachment] as const,
    ),
  );

  return persistedAttachments.map((attachment) => {
    const localAttachment = localByUploadId.get(attachment.fileUploadId);
    if (!localAttachment?.url?.startsWith("blob:")) return attachment;
    return {
      ...attachment,
      url: localAttachment.url,
      size: attachment.size || localAttachment.size,
      storageUri: attachment.storageUri || localAttachment.storageUri,
    };
  });
}

function reconcilePersistedMessagesWithLocal(
  persistedMessages: Message[],
  localMessages: Message[],
): Message[] {
  return persistedMessages.map((persistedMessage, persistedIndex) => {
    const roleOccurrence = countMessagesByRole(
      persistedMessages,
      persistedMessage.role,
      persistedIndex,
    );
    const localMatch =
      localMessages.find(
        (localMessage) => localMessage.id === persistedMessage.id,
      ) ??
      localMessages.find((localMessage) => {
        if (localMessage.role !== persistedMessage.role) return false;
        if (messageMergeKey(localMessage) !== messageMergeKey(persistedMessage))
          return false;
        return (
          countMatchingMessages(localMessages, localMessage) ===
          countMatchingMessages(persistedMessages, persistedMessage)
        );
      }) ??
      findMessageByRoleOccurrence(
        localMessages,
        persistedMessage.role,
        roleOccurrence,
      );

    if (
      !localMatch?.attachments?.length ||
      !persistedMessage.attachments?.length
    ) {
      return persistedMessage;
    }

    return {
      ...persistedMessage,
      attachments: mergeMessageAttachments(
        persistedMessage.attachments,
        localMatch.attachments,
      ),
    };
  });
}

function mergeSessionMessages(
  persistedMessages: Message[],
  localMessages: Message[],
): Message[] {
  const reconciledPersistedMessages = reconcilePersistedMessagesWithLocal(
    persistedMessages,
    localMessages,
  );
  const merged = [...reconciledPersistedMessages];
  localMessages.forEach((localMessage, index) => {
    if (
      !shouldKeepLocalMessage(
        reconciledPersistedMessages,
        localMessages,
        localMessage,
        index,
      )
    ) {
      return;
    }
    if (merged.some((message) => message.id === localMessage.id)) return;
    merged.push(localMessage);
  });
  return merged.sort(compareMessages);
}

function getSessionLocalMessages(
  state: ChatState,
  sessionId: string,
): Message[] {
  return state.messageIds
    .map((id) => state.messagesMap[id])
    .filter(
      (message): message is Message => message?.conversationId === sessionId,
    );
}

function parseWorkTitleUpdatedPayload(
  data: SSEMessageEvent,
): BackendWorkTitleUpdatedPayload | null {
  const payload = data.payload;
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return null;
  }
  const record = payload as Record<string, unknown>;
  if (
    typeof record.project_id !== "string" ||
    typeof record.project_name !== "string"
  ) {
    return null;
  }
  return {
    project_id: record.project_id,
    project_name: record.project_name,
    task_id: typeof record.task_id === "string" ? record.task_id : undefined,
    task_title:
      typeof record.task_title === "string" ? record.task_title : undefined,
    session_id:
      typeof record.session_id === "string"
        ? record.session_id
        : (data.session_id ?? ""),
    session_title:
      typeof record.session_title === "string"
        ? record.session_title
        : undefined,
  };
}

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
  #sseClient: FetchSSEClient | null = null;
  #messageLoadPromises = new Map<string, Promise<void>>();

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

  sendMessage = async (
    content: string,
    attachments?: Attachment[],
    metadata?: MessageMetadata,
  ) => {
    // 仅上传附件而无文字时后端会报错，必须要求有文本内容
    if (!content.trim()) return;

    const state = this.#get();
    // 中文注释：生成中禁止再次发送，避免新的请求把上一条流式响应直接顶掉。
    if (state.isGenerating) return;
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
        attachments: mapOutgoingAttachments(attachments),
        metadata: metadata?.composerTokens
          ? { extra: { composerTokens: metadata.composerTokens } }
          : undefined,
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
      attachments: mapComposerAttachments(attachments),
      metadata,
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

  sendProjectMessage = async (
    content: string,
    projectId?: string | null,
    attachments?: Attachment[],
    metadata?: MessageMetadata,
  ) => {
    const trimmed = content.trim();
    if (!trimmed || !projectId) return null;
    if (this.#get().isGenerating) return null;

    try {
      const res = await workApi.newMessage({
        content: trimmed,
        project_id: projectId,
        attachments: mapOutgoingAttachments(attachments),
      });
      const data = res.data.data;
      if (!data?.project_id || !data?.task_id || !data?.session_id) return null;

      (this.#set as (partial: Record<string, unknown>) => void)({
        activeProjectId: data.project_id,
        activeTaskDetailProjectId: data.project_id,
        activeTaskDetailTaskId: data.task_id,
        activeTaskDetailSessionId: data.session_id,
        // 先标记这个新 session 正在接管流式响应，避免切页副作用把旧消息列表提前刷回来。
        pendingBootstrapSessionId: data.session_id,
        currentView: "taskDetail",
        activeProjectTab: "chat",
        conversationListOpen: false,
        inputText: "",
        inputAttachments: [],
      });

      await this.startSessionResponseStream(
        data.session_id,
        trimmed,
        attachments,
        metadata,
      );

      const fullState = this.#fullGet() as {
        fetchProjectDetail?: (projectId: string) => Promise<void>;
      };
      await fullState.fetchProjectDetail?.(data.project_id);
      return data;
    } catch (err) {
      console.error("sendProjectMessage error:", err);
      return null;
    }
  };

  startSessionResponseStream = async (
    sessionId: string,
    content: string,
    attachments?: Attachment[],
    metadata?: MessageMetadata,
  ) => {
    const trimmed = content.trim();
    if (!sessionId || !trimmed) return;

    const state = this.#get();
    if (state.isGenerating) return;
    if (state.activeSessionId === sessionId && state.isGenerating) {
      if (state.pendingBootstrapSessionId === sessionId) {
        this.#set({ pendingBootstrapSessionId: null });
      }
      return;
    }

    const now = Date.now();
    this.#set({
      activeSessionId: sessionId,
      streamingMessageId: null,
      isGenerating: true,
      pendingBootstrapSessionId: sessionId,
      inputText: "",
      inputAttachments: [],
    });

    const fallbackUserMsg: Message = {
      id: `msg-user-${now}`,
      conversationId: sessionId,
      role: "user",
      content: trimmed,
      timestamp: now,
      attachments: mapComposerAttachments(attachments),
      metadata,
    };
    const assistantMsg: Message = {
      id: `msg-assistant-${now}`,
      conversationId: sessionId,
      role: "assistant",
      content: "",
      timestamp: now + 100,
    };

    const messagesMap: Record<string, Message> = {};
    const messageIds: string[] = [];
    for (const message of [fallbackUserMsg, assistantMsg]) {
      messagesMap[message.id] = message;
      messageIds.push(message.id);
    }

    this.#set({
      activeSessionId: sessionId,
      messagesMap,
      messageIds,
      streamingMessageId: assistantMsg.id,
      isGenerating: true,
      pendingBootstrapSessionId: null,
      inputText: "",
      inputAttachments: [],
    });

    this.#startSSE(sessionId, assistantMsg.id);
  };

  #startSSE = async (
    sessionId: string,
    assistantMsgId: string,
    replay = false,
  ) => {
    if (this.#sseClient) {
      this.#sseClient.close();
      this.#sseClient = null;
    }

    const url = `${API_BASE_URL}/SessionEvents`;
    const token = await getValidJwtToken();
    if (!token) {
      this.#finishStream();
      return;
    }
    const client = new FetchSSEClient(url, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
      body: { session_id: sessionId, ...(replay ? { replay: true } : {}) },
      onMessage: (event) => {
        try {
          const data = JSON.parse(event.data) as SSEMessageEvent;
          const eventType = event.type ?? data.type;

          if (eventType === "work.title.updated") {
            const workTitlePayload = parseWorkTitleUpdatedPayload(data);
            if (workTitlePayload) {
              const fullState = this.#fullGet() as {
                applyWorkTitleUpdated?: (
                  payload: BackendWorkTitleUpdatedPayload,
                ) => void;
              };
              fullState.applyWorkTitleUpdated?.(workTitlePayload);
            }
            return;
          }

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

          if (
            eventType === "run.completed" ||
            eventType === "run.failed" ||
            eventType === "run.cancelled"
          ) {
            this.#finishStream();
            this.#sseClient?.close();
            this.#sseClient = null;
            // 清除取消标记
            this.#set({ cancellingSessionId: null });
            // 会话结束后回拉历史消息，确保持久化 usage 能立即参与页面汇总展示。
            void this.loadConversationMessages(sessionId, {
              resumeStream: false,
            });
            const fullState = this.#fullGet() as {
              activeTaskDetailProjectId?: string | null;
              fetchProjectDetail?: (projectId: string) => Promise<void>;
              projects?: Array<{
                id: string;
                tasks: Array<{ sessionId?: string }>;
              }>;
            };
            const projectId = resolveProjectIdForSession(fullState, sessionId);
            if (projectId) {
              void fullState.fetchProjectDetail?.(projectId);
            }
          }
        } catch (err) {
          // 正文只接受 run.completed 的最终结果，解析失败的流片段不再兜底写入正文。
          console.error("SSE message parse error:", err);
        }
      },
      onError: (err) => {
        console.error("SSE error:", err);
        this.#finishStream();
      },
    });

    this.#set({ streamCancelRef: () => client.close() });
    void client.connect();
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

    // 标记此 session 正在取消 + 通知后端真实取消 agent 执行（异步，不阻塞 UI 清理）
    if (state.activeSessionId) {
      this.#set({ cancellingSessionId: state.activeSessionId });
      sessionApi
        .cancelSessionRun({ session_id: state.activeSessionId })
        .catch(() => {
          // 取消 API 调用失败不影响前端状态重置
        });
    }

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

  loadConversationMessages = async (
    sessionId: string,
    options?: { resumeStream?: boolean },
  ) => {
    if (this.#get().pendingBootstrapSessionId === sessionId) return;
    const loading = this.#messageLoadPromises.get(sessionId);
    if (loading) return loading;

    const loadPromise = this.#loadConversationMessages(
      sessionId,
      options,
    ).finally(() => {
      this.#messageLoadPromises.delete(sessionId);
    });
    this.#messageLoadPromises.set(sessionId, loadPromise);
    return loadPromise;
  };

  #fetchAllConversationMessages = async (sessionId: string) => {
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

  #loadConversationMessages = async (
    sessionId: string,
    options?: { resumeStream?: boolean },
  ) => {
    try {
      const shouldCheckRuntime = options?.resumeStream !== false;
      let runtimeStatus: string | undefined;
      if (shouldCheckRuntime) {
        try {
          const sessionRes = await sessionApi.get({ session_id: sessionId });
          runtimeStatus = sessionRes.data.data?.runtime_status;
        } catch (err) {
          console.error("loadConversationMessages get session error:", err);
        }
      }

      const stateBeforeLoad = this.#get();
      const optimisticCountBeforeLoad = getSessionLocalMessages(
        stateBeforeLoad,
        sessionId,
      ).filter(isOptimisticMessage).length;
      const items = await this.#fetchAllConversationMessages(sessionId);
      const persistedMessages = items.map(mapBackendMessage);
      const state = this.#get();
      if (state.pendingBootstrapSessionId === sessionId) return;
      const localSessionMessages = getSessionLocalMessages(state, sessionId);
      const optimisticCountAfterLoad =
        localSessionMessages.filter(isOptimisticMessage).length;
      // 如果请求发出后，这个 session 在本地新插入了 optimistic 消息，说明当前返回值已经过时，
      // 直接丢弃，避免把“刚发出去的用户消息”又覆盖没。
      if (optimisticCountAfterLoad > optimisticCountBeforeLoad) return;
      // 生成中或刚完成但本地仍有 optimistic 消息时，优先保留本地消息；
      // 同时过滤掉空内容 assistant 占位，避免在落库后与真实 assistant 重复显示。
      const shouldPreserveLocalMessages =
        state.activeSessionId === sessionId &&
        (state.isGenerating || optimisticCountAfterLoad > 0);
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
      const shouldResumeStream =
        runtimeStatus === "responding" &&
        state.cancellingSessionId !== sessionId &&
        !(
          state.isGenerating &&
          state.activeSessionId === sessionId &&
          state.streamingMessageId !== null
        );
      const resumeMessage: Message | undefined = shouldResumeStream
        ? {
            id: `msg-assistant-resume-${Date.now()}`,
            conversationId: sessionId,
            role: "assistant",
            content: "",
            timestamp: Date.now(),
          }
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

      this.#set({
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
        this.#startSSE(sessionId, resumeMessage.id, true);
      }
    } catch (err) {
      console.error("loadConversationMessages error:", err);
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
      pendingBootstrapSessionId: null,
      streamCancelRef: null,
    });
  };

  setInputText = (text: string) => {
    this.#set({ inputText: text });
  };

  clearComposerInput = () => {
    const state = this.#get();
    revokeLocalAttachmentUrls(state.inputAttachments);
    this.#set({ inputText: "", inputAttachments: [] });
  };

  appendSkillDirective = (skillName: string) => {
    this.#set((state) => ({
      inputText: appendSkillDirectiveToInput(state.inputText, skillName),
    }));
  };

  replaceSkillDirective = (skillName: string) => {
    this.#set((state) => ({
      inputText: replaceSkillDirectiveInInput(state.inputText, skillName),
    }));
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

  addUploadedAttachment = async (projectId: string, file: File) => {
    const response = await projectFileApi.upload({
      projectId,
      projectPublicId: projectId,
      file,
    });
    const payload = response.data;
    const attachmentId = `att-${Date.now()}`;
    const previewUrl = file.type.startsWith("image/")
      ? URL.createObjectURL(file)
      : undefined;

    const attachment: Attachment = {
      id: attachmentId,
      type: file.type.startsWith("image/") ? "image" : "file",
      name: payload.original_name || payload.filename || file.name,
      size: payload.file_size ?? payload.size ?? file.size,
      url: previewUrl,
      file,
      path: payload.public_id || payload.storage_uri || payload.path,
      fileUploadId: payload.public_id,
      mimeType: payload.mime_type || file.type,
      storageUri: payload.storage_uri,
    };

    this.#set((state) => ({
      inputAttachments: [...state.inputAttachments, attachment],
    }));

    return { attachment, message: response.message };
  };

  removeAttachment = (id: string) => {
    const state = this.#get();
    const att = state.inputAttachments.find((a) => a.id === id);
    if (att?.url?.startsWith("blob:")) URL.revokeObjectURL(att.url);
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

  submitQuestionAnswer = async (
    messageId: string,
    requestId: string,
    answers: string[][],
  ) => {
    const state = this.#get();
    const message = state.messagesMap[messageId];
    const sessionId = message?.conversationId || state.activeSessionId;
    if (!sessionId) return;

    this.#dispatchChat({
      type: "updateQuestionStatus",
      messageId,
      requestId,
      status: "submitting",
      answers,
      error: undefined,
    });

    try {
      await sessionApi.submitQuestionAnswer({
        session_id: sessionId,
        request_id: requestId,
        answers,
      });
      this.#dispatchChat({
        type: "updateQuestionStatus",
        messageId,
        requestId,
        status: "answered",
        answers,
        error: undefined,
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
      const {
        messageId,
        requestId,
        status,
        action: approvalAction,
        reason,
        error,
      } = action;
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
        tc.id === toolCallId
          ? { ...tc, status, ...(result ? { result } : {}) }
          : tc,
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
