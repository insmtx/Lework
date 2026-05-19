export type MessageRole = "user" | "assistant" | "system" | "tool";

export type ToolCallStatus = "pending" | "running" | "success" | "error";

export type ToolCall = {
	id: string;
	name: string;
	arguments: Record<string, unknown>;
	status: ToolCallStatus;
	result?: Record<string, unknown>;
	duration?: number;
};

export type MessageMetadata = {
	model?: string;
	tokens?: number;
	latency?: number;
};

export type Message = {
	id: string;
	conversationId: string;
	role: MessageRole;
	content: string;
	timestamp: number;
	toolCalls?: ToolCall[];
	thinking?: string;
	metadata?: MessageMetadata;
};

export type Attachment = {
	id: string;
	type: "image" | "file";
	name: string;
	size: number;
	url?: string;
	file?: File;
};

export type ModelOption = {
	id: string;
	label: string;
	provider: string;
};
