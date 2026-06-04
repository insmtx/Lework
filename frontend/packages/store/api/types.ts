export type BackendBaseResponse = {
	code: number;
	message: string;
};

export type BackendErrorResponse = BackendBaseResponse & {
	details?: string;
};

export type BackendPaginatedResponse<T> = BackendBaseResponse & {
	data: {
		total: number;
		page: number;
		items: T[];
	};
};

export type BackendDataResponse<T> = BackendBaseResponse & {
	data: T;
};

export type BackendSession = {
	session_id: string;
	type: string;
	user_id: number;
	assistant_id: number;
	assistant_code: string;
	status: string;
	title: string;
	message_count: number;
	last_message_at?: string;
	metadata?: BackendSessionMetadata;
	expired_at?: string;
	created_at: string;
	updated_at: string;
};

export type BackendSessionMetadata = {
	user_agent?: string;
	ip_address?: string;
	tags?: string[];
	extra?: Record<string, unknown>;
};

export type BackendMessage = {
	id: string;
	session_id?: string;
	conversation_id?: string;
	role: string;
	content: string;
	timestamp: number;
	message_type: string;
	sequence: number;
	metadata?: BackendMessageMetadata;
	chunks?: BackendMessageChunk[];
	created_at: string;
};

export type BackendSessionEvent = {
	type: string;
	session_id?: string;
	sequence?: number;
	timestamp?: number;
	payload?: BackendSessionEventPayloadLike;
};

export type BackendMessageChunk = BackendSessionEvent | string;

export type BackendMessageMetadata = {
	model?: string;
	tokens?: number;
	latency?: number;
	image_url?: string;
	file_url?: string;
	file_name?: string;
	language?: string;
	extra?: Record<string, unknown>;
};

export type BackendToolCall = {
	id: string;
	name: string;
	arguments?: Record<string, unknown>;
	status: string;
	result?: unknown;
	duration?: number;
};

export type BackendTodoStatus = "pending" | "in_progress" | "completed" | "cancelled";

export type BackendRuntimeTodoItem = {
	id?: string;
	title?: string;
	status?: BackendTodoStatus | string;
	priority?: string;
};

export type BackendSessionArtifactPayload = {
	artifact_id?: string;
	title?: string;
	filename?: string;
	description?: string;
	artifact_type?: string;
	mime_type?: string;
	file_size?: number;
	sha256?: string;
};

export type BackendApprovalRequestPayload = {
	request_id?: string;
	tool_name?: string;
	tool_call_id?: string;
	description?: string;
	arguments?: Record<string, unknown>;
	metadata?: Record<string, unknown>;
};

export type BackendApprovalDecisionPayload = {
	request_id?: string;
	action?: string;
	reason?: string;
};

export type BackendDigitalAssistant = {
	id: number;
	code: string;
	name: string;
	description?: string;
	avatar?: string;
	org_id: number;
	owner_id: number;
	status: string;
	system_prompt?: string;
	config?: BackendAssistantConfig;
	version: number;
	created_at: string;
	updated_at: string;
};

export type BackendAssistantConfig = {
	llm_config?: BackendLLMConfig;
	skills?: BackendSkillRef[];
	channels?: BackendChannelRef[];
	knowledge?: BackendKnowledgeRef[];
	memory_config?: BackendMemoryConfig;
	policies_config?: BackendPolicyConfig;
	runtime_config?: BackendRuntimeConfig;
};

export type BackendLLMConfig = {
	type?: string;
};

export type BackendSkillRef = {
	skill_code?: string;
	version?: string;
};

export type BackendChannelRef = {
	type?: string;
};

export type BackendKnowledgeRef = {
	type?: string;
	repo?: string;
	dataset_id?: string;
};

export type BackendMemoryConfig = {
	type?: string;
};

export type BackendPolicyConfig = {
	type?: string;
};

export type BackendRuntimeConfig = {
	type?: string;
};

export type SSEMessageEvent = BackendSessionEvent & {
	message_id?: string;
	conversation_id?: string;
	role?: string;
	content?: string;
	chunk?: string;
	status?: string;
	thinking?: string;
	tool_calls?: BackendToolCall[];
	todos?: BackendRuntimeTodoItem[];
	metadata?: BackendMessageMetadata;
};

export type BackendSessionEventPayload = {
	id?: string;
	message_id?: string;
	request_id?: string;
	tool_name?: string;
	tool_call_id?: string;
	action?: string;
	role?: string;
	content?: string;
	thinking?: string;
	tool_calls?: BackendToolCall[];
	todos?: BackendRuntimeTodoItem[];
	name?: string;
	arguments?: Record<string, unknown>;
	result?: unknown;
	error?: string;
	is_error?: boolean;
	status?: string;
	duration?: number;
	elapsed_ms?: number;
	run_id?: string;
	message?: string;
	usage?: {
		input_tokens?: number;
		output_tokens?: number;
		total_tokens?: number;
	};
	metadata?: BackendMessageMetadata;
	events?: BackendSessionEvent[];
	artifacts?: BackendSessionArtifactPayload[];
	approval_request?: BackendApprovalRequestPayload;
	approval_decision?: BackendApprovalDecisionPayload;
	artifact_id?: string;
	title?: string;
	filename?: string;
	description?: string;
	artifact_type?: string;
	mime_type?: string;
	file_size?: number;
	sha256?: string;
	input_tokens?: number;
	output_tokens?: number;
	total_tokens?: number;
	model?: string;
};

export type BackendSessionEventPayloadLike = BackendSessionEventPayload | BackendRuntimeTodoItem[];

export type SSEEventPayload = BackendSessionEventPayloadLike;

export type BackendProject = {
	public_id: string;
	name: string;
	description?: string;
	objective?: string;
	status?: string;
	owner_id?: number;
	org_id?: number;
	metadata?: Record<string, unknown>;
	created_at: string;
	updated_at: string;
};

export type BackendTask = {
	id: number;
	public_id: string;
	title: string;
	description?: string;
	status?: string;
	project_id: number;
	assignee_id?: number;
	task_type?: string;
	deadline?: string;
	metadata?: Record<string, unknown>;
	created_at: string;
	updated_at: string;
};

export type BackendArtifact = {
	artifact_id: string;
	title: string;
	filename?: string;
	description?: string;
	artifact_type: string;
	mime_type?: string;
	file_size?: number;
	sha256?: string;
};

export type BackendProjectMemberItem = {
	member_id: number;
	member_type: string;
	member_role: string;
	joined_at: string;
	name?: string;
	avatar_url?: string;
};

export type BackendProjectTaskItem = BackendTask & {
	session?: BackendSession;
};

export type BackendProjectDetail = BackendProject & {
	session?: BackendSession;
	tasks: BackendProjectTaskItem[];
	artifacts: BackendArtifact[];
	members: BackendProjectMemberItem[];
};

export type BackendNewMessageData = {
	project_id: string;
	task_id: string;
	session_id: string;
};
