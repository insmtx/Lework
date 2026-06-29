import { apiClient } from "./client";
import { API_BASE_URL } from "./config";
import type {
	BackendDataResponse,
	BackendMessage,
	BackendMessageChunk,
	BackendPaginatedResponse,
	BackendSession,
} from "./types";

export type CreateSessionParams = {
	type: string;
	title?: string;
	assistant_id?: number;
	assistant_code?: string;
	session_id?: string;
	user_id?: number;
	expired_at?: string;
	metadata?: {
		user_agent?: string;
		ip_address?: string;
		tags?: string[];
		extra?: Record<string, unknown>;
	};
};

export type UpdateSessionParams = {
	session_id: string;
	title?: string;
	expired_at?: string;
	metadata?: {
		user_agent?: string;
		ip_address?: string;
		tags?: string[];
		extra?: Record<string, unknown>;
	};
};

export type ListSessionsParams = {
	page?: number;
	per_page?: number;
	type?: string;
	status?: string;
	keyword?: string;
	assistant_id?: number;
	assistant_code?: string;
	user_id?: number;
};

export type GetSessionParams = {
	id?: number;
	session_id?: string;
};

export type AddMessageParams = {
	session_id: string;
	role: string;
	content: string;
	message_type?: string;
	attachments?: {
		file_upload_id: string;
		name: string;
		mime_type: string;
	}[];
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
};

export type SubmitQuestionAnswerParams = {
	session_id: string;
	request_id: string;
	answers: string[][];
};

const SESSION_ENDPOINTS = {
	create: "/CreateSession",
	list: "/ListSessions",
	get: "/GetSession",
	update: "/UpdateSession",
	delete: "/DeleteSession",
	addMessage: "/AddMessage",
	getMessages: "/GetSessionMessages",
	deleteMessage: "/DeleteMessage",
	clearMessages: "/ClearSessionMessages",
	sessionEvents: "/SessionEvents",
};

export const sessionApi = {
	create: (params: CreateSessionParams) =>
		apiClient.post<BackendDataResponse<BackendSession>>(SESSION_ENDPOINTS.create, params),

	list: (params: ListSessionsParams) =>
		apiClient.post<BackendPaginatedResponse<BackendSession>>(SESSION_ENDPOINTS.list, params),

	get: (params: GetSessionParams) =>
		apiClient.post<BackendDataResponse<BackendSession>>(SESSION_ENDPOINTS.get, params),

	update: (params: UpdateSessionParams) =>
		apiClient.post<BackendDataResponse<BackendSession>>(SESSION_ENDPOINTS.update, params),

	delete: (sessionId: string) =>
		apiClient.post<BackendDataResponse<null>>(SESSION_ENDPOINTS.delete, { session_id: sessionId }),

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
					...(params.reason ? { reason: params.reason } : {}),
				},
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
				},
			},
		),

	getSessionEventsURL: (_sessionId?: string, _lastSequence?: number) =>
		`${API_BASE_URL}/SessionEvents`,

	cancelSessionRun: (params: { session_id: string; reason?: string }) =>
		apiClient.post<BackendDataResponse<{ session_id: string; status: string }>>(
			`/CancelSessionRun`,
			{
				session_id: params.session_id,
				reason: params.reason,
			},
		),
};
