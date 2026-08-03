/**
 * 发送侧 metadata 编解码。
 *
 * 可以做：从 composer metadata 提取 assistant_ids、压成后端 metadata.extra。
 * 不可以做：发消息、开流、改 UI 状态。
 */
import type { BackendMessageMetadata } from "../../api/types";
import type { MessageMetadata } from "../../types/chat";

/**
 * 从 composer metadata 提取显式选择的 AI 员工 id 列表。
 * 只有用户点选了助手时才传 assistant_ids，避免覆盖后端默认分配。
 */
export function extractAssistantIdsFromMetadata(metadata?: MessageMetadata): string[] | undefined {
	const assistantIds = Array.from(
		new Set(
			(metadata?.composerTokens ?? [])
				.filter((token) => token.kind === "assistant" && token.id?.trim())
				.map((token) => token.id?.trim())
				.filter((id): id is string => Boolean(id)),
		),
	);
	return assistantIds.length > 0 ? assistantIds : undefined;
}

/**
 * 将前端 MessageMetadata 压成后端 metadata.extra。
 * 用于透传输入框展示态，避免新增后端字段。
 */
export function buildBackendMessageMetadata(
	metadata?: MessageMetadata,
): BackendMessageMetadata | undefined {
	if (!metadata) return undefined;

	const extra: Record<string, unknown> = {};
	if (metadata.composerTokens?.length) extra.composerTokens = metadata.composerTokens;
	if (metadata.displayContent?.trim()) extra.displayContent = metadata.displayContent;
	if (metadata.displayComposerTokens?.length) {
		extra.displayComposerTokens = metadata.displayComposerTokens;
	}
	if (metadata.invokedAssistant) extra.invokedAssistant = metadata.invokedAssistant;

	return Object.keys(extra).length > 0 ? { extra } : undefined;
}
