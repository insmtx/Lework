import type { ComposerToken, Message, MessageMetadata, MessageUsage } from "../types/chat";

type MetadataSource = {
	model?: string;
	tokens?: number;
	latency?: number;
	composerTokens?: ComposerToken[];
	displayContent?: string;
	displayComposerTokens?: ComposerToken[];
	invokedAssistant?: MessageMetadata["invokedAssistant"];
	extra?: Record<string, unknown>;
};

function pickString(...values: unknown[]): string | undefined {
	for (const value of values) {
		if (typeof value === "string" && value.trim()) {
			return value.trim();
		}
	}
	return undefined;
}

function pickNumber(...values: unknown[]): number | undefined {
	for (const value of values) {
		if (typeof value === "number" && Number.isFinite(value)) {
			return value;
		}
	}
	return undefined;
}

function pickComposerTokens(...values: unknown[]): ComposerToken[] | undefined {
	for (const value of values) {
		if (!Array.isArray(value)) continue;
		const tokens = value.filter((item): item is ComposerToken => {
			if (typeof item !== "object" || item === null) return false;
			const token = item as Partial<ComposerToken>;
			return (
				(token.kind === "assistant" || token.kind === "skill" || token.kind === "reference") &&
				typeof token.label === "string" &&
				typeof token.start === "number" &&
				typeof token.end === "number"
			);
		});
		if (tokens.length > 0) return tokens;
	}
	return undefined;
}

function pickInvokedAssistant(...values: unknown[]): MessageMetadata["invokedAssistant"] {
	for (const value of values) {
		if (typeof value !== "object" || value === null) continue;
		const assistant = value as Partial<NonNullable<MessageMetadata["invokedAssistant"]>>;
		if (typeof assistant.name !== "string" || !assistant.name.trim()) continue;
		const result: NonNullable<MessageMetadata["invokedAssistant"]> = {
			name: assistant.name.trim(),
		};
		if (typeof assistant.id === "string" && assistant.id.trim()) result.id = assistant.id.trim();
		if (typeof assistant.avatarUrl === "string" && assistant.avatarUrl.trim()) {
			result.avatarUrl = assistant.avatarUrl.trim();
		}
		return result;
	}
	return undefined;
}

/** 从 run.completed 的起止时间计算单次回复耗时（毫秒）。 */
export function latencyFromRunCompletedTimes(
	startedAt?: string,
	completedAt?: string,
): number | undefined {
	if (!startedAt || !completedAt) return undefined;
	const startMs = Date.parse(startedAt);
	const endMs = Date.parse(completedAt);
	if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs < startMs) {
		return undefined;
	}
	return endMs - startMs;
}

/** 归一化消息 metadata，合并 extra 与 usage 中的展示字段。 */
export function buildMessageMetadata(
	metadata?: MetadataSource,
	usage?: MessageUsage,
): MessageMetadata | undefined {
	const extra = metadata?.extra;
	const model = pickString(metadata?.model, extra?.model, extra?.model_name);
	const tokens = pickNumber(metadata?.tokens, extra?.tokens, usage?.totalTokens);
	const latency = pickNumber(metadata?.latency, extra?.latency, extra?.latency_ms);
	const composerTokens = pickComposerTokens(metadata?.composerTokens, extra?.composerTokens);
	const displayContent = pickString(metadata?.displayContent, extra?.displayContent);
	const displayComposerTokens = pickComposerTokens(
		metadata?.displayComposerTokens,
		extra?.displayComposerTokens,
	);
	const invokedAssistant = pickInvokedAssistant(
		metadata?.invokedAssistant,
		extra?.invokedAssistant,
	);

	if (
		!model &&
		tokens === undefined &&
		latency === undefined &&
		!composerTokens &&
		!displayContent &&
		!displayComposerTokens &&
		!invokedAssistant
	) {
		return undefined;
	}
	return {
		model,
		tokens,
		latency,
		composerTokens,
		displayContent,
		displayComposerTokens,
		invokedAssistant,
	};
}

/** 将 usage 中的 token 总数回填到 metadata，便于统一展示逻辑。 */
export function enrichAssistantMessageMetrics(message: Message): Message {
	if (message.role !== "assistant") {
		return message;
	}
	const metadata = buildMessageMetadata(message.metadata, message.usage);
	if (!metadata) {
		const { metadata: _removed, ...rest } = message;
		return rest;
	}
	return { ...message, metadata };
}
