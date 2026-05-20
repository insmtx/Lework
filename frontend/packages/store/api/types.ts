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
	id: number;
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
	conversation_id: string;
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
	payload?: BackendSessionEventPayload;
};

export type BackendMessageChunk = BackendSessionEvent;

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
	arguments: Record<string, unknown>;
	status: string;
	result?: Record<string, unknown>;
	duration?: number;
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
	metadata?: BackendMessageMetadata;
};

export type BackendSessionEventPayload = {
	message_id?: string;
	tool_call_id?: string;
	role?: string;
	content?: string;
	thinking?: string;
	tool_calls?: BackendToolCall[];
	name?: string;
	arguments?: Record<string, unknown>;
	result?: unknown;
	status?: string;
	run_id?: string;
	message?: string;
	input_tokens?: number;
	output_tokens?: number;
	total_tokens?: number;
	model?: string;
};

export type SSEEventPayload = BackendSessionEventPayload;
