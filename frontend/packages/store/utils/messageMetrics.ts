import type { Message, MessageMetadata, MessageUsage } from "../types/chat";
import { formatLatency, formatTokenCount } from "./format";

type MetadataSource = {
	model?: string;
	tokens?: number;
	latency?: number;
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

	if (!model && tokens === undefined && latency === undefined) {
		return undefined;
	}
	return { model, tokens, latency };
}

/** 读取单条 assistant 消息的 tokens / latency 原始指标。 */
function getAssistantMessageMetrics(message: Message): MessageMetadata | undefined {
	if (message.role !== "assistant") return undefined;
	return buildMessageMetadata(message.metadata, message.usage);
}

/** 生成消息 footer 展示片段：仅耗时与 token 用量。 */
export function getAssistantMessageFooterSegments(message: Message): string[] {
	const metrics = getAssistantMessageMetrics(message);
	if (!metrics) return [];

	const segments: string[] = [];
	if (metrics.latency !== undefined) {
		segments.push(formatLatency(metrics.latency));
	}
	if (metrics.tokens !== undefined) {
		segments.push(`${formatTokenCount(metrics.tokens)} Tokens`);
	}
	return segments;
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
