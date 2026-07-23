import type { Message, RuntimeTodoItem } from "@leros/store/types/chat";

function getAssistantTodos(message: Message | undefined): RuntimeTodoItem[] | undefined {
	if (message?.role !== "assistant") return undefined;
	return message.todos?.length ? message.todos : undefined;
}

function forceCompleteTodos(todos: RuntimeTodoItem[]): RuntimeTodoItem[] {
	if (todos.every((todo) => todo.status === "completed")) {
		return todos;
	}
	return todos.map((todo) =>
		todo.status === "completed" ? todo : { ...todo, status: "completed" as const },
	);
}

function isAssistantRunFinished(
	message: Message | undefined,
	streamingMessageId: string | null,
): boolean {
	if (message?.role !== "assistant") return false;
	if (message.status === "failed") return false;
	if (
		message.status === "streaming" ||
		message.status === "waiting" ||
		message.status === "sending"
	) {
		return false;
	}
	if (streamingMessageId === message.id) return false;
	return (
		message.status === "completed" ||
		Boolean(message.content?.trim() || message.processSteps?.length)
	);
}

function resolveAssistantTodos(
	message: Message | undefined,
	streamingMessageId: string | null,
): RuntimeTodoItem[] | undefined {
	const todos = getAssistantTodos(message);
	if (!todos) return undefined;
	if (isAssistantRunFinished(message, streamingMessageId)) {
		return forceCompleteTodos(todos);
	}
	return todos;
}

export function getLatestAssistantTodos(
	messagesMap: Record<string, Message>,
	messageIds: string[],
	sessionId: string | null | undefined,
	streamingMessageId: string | null,
): RuntimeTodoItem[] | undefined {
	if (!sessionId) return undefined;

	const sessionMessages = messageIds
		.map((id) => messagesMap[id])
		.filter((message): message is Message => message?.conversationId === sessionId);

	if (streamingMessageId) {
		const streamingMessage = messagesMap[streamingMessageId];
		const streamingTodos = resolveAssistantTodos(streamingMessage, streamingMessageId);
		if (streamingTodos) return streamingTodos;
	}

	for (let index = sessionMessages.length - 1; index >= 0; index -= 1) {
		const message = sessionMessages[index];
		const todos = resolveAssistantTodos(message, streamingMessageId);
		if (todos) return todos;
	}

	return undefined;
}
