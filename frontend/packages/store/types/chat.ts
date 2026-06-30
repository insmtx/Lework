export type MessageRole = "user" | "assistant" | "system" | "tool";

export type ToolCallStatus = "pending" | "running" | "success" | "error";

export type TodoStatus = "pending" | "in_progress" | "completed" | "cancelled";

export type ApprovalAction = "approve" | "deny" | "always";

export type ApprovalStatus = "pending" | "approved" | "denied" | "always" | "submitting" | "error";

export type QuestionStatus = "pending" | "answered" | "submitting" | "error";

export type ExecutionMode = "default" | "plan";

export type QuestionOption = {
	label: string;
	description?: string;
};

export type QuestionItem = {
	question: string;
	header?: string;
	options: QuestionOption[];
	multiple: boolean;
	custom: boolean;
};

export type QuestionRequest = {
	requestId: string;
	questions: QuestionItem[];
	toolCallId?: string;
	messageId?: string;
	interactionType?: string;
	plan?: PlanHandoff;
	metadata?: Record<string, unknown>;
	status: QuestionStatus;
	answers?: string[][];
	error?: string;
};

export type PlanHandoff = {
	content?: string;
	filePath?: string;
	error?: string;
};

export type ToolCall = {
	id: string;
	name: string;
	arguments: Record<string, unknown>;
	status: ToolCallStatus;
	result?: unknown;
	duration?: number;
};

export type MessageProcessStep =
	| {
			id: string;
			type: "thinking";
			content: string;
	  }
	| {
			id: string;
			type: "tool_call";
			toolCallId: string;
	  };

export type RuntimeTodoItem = {
	id: string;
	title: string;
	status: TodoStatus;
	priority?: string;
};

export type ApprovalRequest = {
	requestId: string;
	toolName: string;
	toolCallId?: string;
	description: string;
	arguments?: Record<string, unknown>;
	metadata?: Record<string, unknown>;
	status: ApprovalStatus;
	action?: ApprovalAction;
	reason?: string;
	error?: string;
};

export type MessageArtifact = {
	id: string;
	name: string;
	title: string;
	description?: string;
	type: "document" | "spreadsheet" | "image";
	artifactType: string;
	mimeType?: string;
	size: string;
	updatedAt?: number;
	downloadUrl: string;
	storageUri?: string;
	sha256?: string;
};

export type MessageMetadata = {
	model?: string;
	tokens?: number;
	latency?: number;
	composerTokens?: ComposerToken[];
};

export type ComposerToken = {
	kind: "assistant" | "skill";
	label: string;
	start: number;
	end: number;
};

export type MessageUsage = {
	inputTokens?: number;
	outputTokens?: number;
	totalTokens?: number;
};

export type MessageAttachment = {
	id: string;
	fileUploadId: string;
	name: string;
	mimeType: string;
	size: number;
	createdAt?: number;
	url?: string;
	storageUri?: string;
};

export type Message = {
	id: string;
	conversationId: string;
	role: MessageRole;
	content: string;
	timestamp: number;
	sequence?: number;
	toolCalls?: ToolCall[];
	todos?: RuntimeTodoItem[];
	approvals?: ApprovalRequest[];
	questions?: QuestionRequest[];
	artifacts?: MessageArtifact[];
	attachments?: MessageAttachment[];
	processSteps?: MessageProcessStep[];
	metadata?: MessageMetadata;
	usage?: MessageUsage;
};

export type Attachment = {
	id: string;
	type: "image" | "file";
	name: string;
	size: number;
	url?: string;
	file?: File;
	path?: string;
	fileUploadId?: string;
	mimeType?: string;
	storageUri?: string;
};

export type ModelOption = {
	id: string;
	label: string;
	provider: string;
};
