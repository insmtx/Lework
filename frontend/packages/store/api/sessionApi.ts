import type { OutgoingMessageAttachment } from "../utils/messageAttachments";
import { apiClient } from "./client";
import { API_BASE_URL } from "./config";
import type {
	BackendDataResponse,
	BackendMessage,
	BackendMessageChunk,
	BackendMessageMetadata,
	BackendNewMessageData,
	BackendPaginatedResponse,
	BackendSession,
} from "./types";

export type GetSessionParams = {
	id?: number;
	session_id?: string;
};

export type AddMessageParams = {
	session_id: string;
	role: string;
	content: string;
	execution_mode?: "default" | "plan";
	assistant_ids?: string[];
	connector_ids?: string[];
	message_type?: string;
	attachments?: OutgoingMessageAttachment[];
	thinking?: string;
	metadata?: {
		source?: string;
		tags?: string[];
		model?: string;
		tokens?: number;
		latency?: number;
		image_url?: string;
		file_url?: string;
		file_name?: string;
		language?: string;
		extra?: Record<string, unknown>;
	};
	usage?: {
		prompt_tokens: number;
		completion_tokens: number;
		total_tokens: number;
	};
	tool_calls?: {
		id: string;
		name: string;
		arguments: Record<string, unknown>;
		status: string;
		result?: Record<string, unknown>;
		duration?: number;
	}[];
	chunks?: BackendMessageChunk[];
};

export type ApprovalDecisionAction = "approve" | "deny" | "always";

export type SubmitApprovalDecisionParams = {
	session_id: string;
	request_id: string;
	action: ApprovalDecisionAction;
	reason?: string;
	assistant_id: string;
};

export type SubmitQuestionAnswerParams = {
	session_id: string;
	request_id: string;
	answers: string[][];
	assistant_id: string;
};

export type CreateInitialMessageParams = {
	content: string;
	execution_mode?: "default" | "plan";
	project_id?: string;
	task_id?: string;
	message_type?: string;
	assistant_ids?: string[];
	connector_ids?: string[];
	metadata?: BackendMessageMetadata;
	attachments?: OutgoingMessageAttachment[];
};

const SESSION_ENDPOINTS = {
	get: "/GetSession",
	addMessage: "/AddMessage",
	getMessages: "/GetSessionMessages",
	deleteMessage: "/DeleteMessage",
	clearMessages: "/ClearSessionMessages",
	sessionEvents: "/SessionEvents",
	createInitialMessage: "/CreateInitialMessage",
};

export const sessionApi = {
	get: (params: GetSessionParams) =>
		apiClient.post<BackendDataResponse<BackendSession>>(SESSION_ENDPOINTS.get, params),

	addMessage: (params: AddMessageParams) =>
		apiClient.post<BackendDataResponse<BackendMessage>>(SESSION_ENDPOINTS.addMessage, params),

	getMessages: (sessionId: string, page?: number, perPage?: number) =>
		apiClient.post<BackendPaginatedResponse<BackendMessage>>(SESSION_ENDPOINTS.getMessages, {
			session_id: sessionId,
			page: page ?? 1,
			per_page: perPage ?? 50,
		}),

	deleteMessage: (messageId: number) =>
		apiClient.post<BackendDataResponse<null>>(SESSION_ENDPOINTS.deleteMessage, {
			message_id: messageId,
		}),

	clearMessages: (sessionId: string) =>
		apiClient.post<BackendDataResponse<null>>(SESSION_ENDPOINTS.clearMessages, {
			session_id: sessionId,
		}),

	submitApprovalDecision: (params: SubmitApprovalDecisionParams) =>
		apiClient.post<BackendDataResponse<{ request_id: string; action: string }>>(
			`/sessions/${encodeURIComponent(params.session_id)}/approvals`,
			{
				type: "approval.decide",
				payload: {
					request_id: params.request_id,
					action: params.action,
					assistant_id: params.assistant_id,
				},
				...(params.reason ? { reason: params.reason } : {}),
			},
		),

	submitQuestionAnswer: (params: SubmitQuestionAnswerParams) =>
		apiClient.post<BackendDataResponse<{ request_id: string; status: string }>>(
			`/sessions/${encodeURIComponent(params.session_id)}/approvals`,
			{
				type: "question.answer",
				payload: {
					request_id: params.request_id,
					answers: params.answers,
					assistant_id: params.assistant_id,
				},
			},
		),

	getSessionEventsURL: (_sessionId?: string, _lastSequence?: number) =>
		`${API_BASE_URL}/SessionEvents`,

	cancelSessionRun: (params: { session_id: string; run_id?: string; reason?: string }) =>
		apiClient.post<BackendDataResponse<{ session_id: string; status: string }>>(
			`/CancelSessionRun`,
			{
				session_id: params.session_id,
				...(params.run_id ? { run_id: params.run_id } : {}),
				reason: params.reason,
			},
		),

	createInitialMessage: (params: CreateInitialMessageParams) =>
		apiClient.post<BackendDataResponse<BackendNewMessageData>>(
			SESSION_ENDPOINTS.createInitialMessage,
			params,
		),
};
