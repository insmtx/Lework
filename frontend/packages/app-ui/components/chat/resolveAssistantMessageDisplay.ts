import {
	type DigitalAssistantItem,
	isSystemDefaultAssistant,
	type ProjectMember,
} from "@leros/store";
import type { ComposerToken, Message } from "@leros/store/types/chat";

export type AssistantMessageDisplay = {
	useDefaultBrand: boolean;
	name: string;
	avatarUrl?: string;
};

const DEFAULT_ASSISTANT_NAME = "Lework";

function getReplyTargetMessageId(runId?: string): string | undefined {
	const match = runId?.match(/^req_(.+)$/);
	return match?.[1]?.trim() || undefined;
}

function findAssistantComposerToken(message?: Message): ComposerToken | undefined {
	const tokens = message?.metadata?.displayComposerTokens?.length
		? message.metadata.displayComposerTokens
		: (message?.metadata?.composerTokens ?? []);
	return tokens.find(
		(token) => token.kind === "assistant" && Boolean(token.id?.trim() || token.label.trim()),
	);
}

function normalizeAssistantNameFromToken(token: ComposerToken): string {
	return token.label.replace(/^@+/, "").trim();
}

function resolveAssistantProfile(
	token: ComposerToken,
	assistants: DigitalAssistantItem[],
	projectMembers?: ProjectMember[],
): { name: string; avatarUrl?: string } {
	const publicId = token.id?.trim();
	if (publicId) {
		const matchedAssistant = assistants.find(
			(assistant) =>
				assistant.publicId === publicId ||
				String(assistant.id) === publicId ||
				assistant.code === publicId,
		);
		if (matchedAssistant) {
			return {
				name: matchedAssistant.name,
				avatarUrl: matchedAssistant.avatar || undefined,
			};
		}

		const matchedMember = projectMembers?.find(
			(member) =>
				member.type === "assistant" &&
				!member.isDefault &&
				!isSystemDefaultAssistant(member.publicId) &&
				(member.publicId === publicId || String(member.memberId) === publicId),
		);
		if (matchedMember) {
			return {
				name: matchedMember.name,
				avatarUrl: matchedMember.avatarUrl,
			};
		}
	}

	const tokenName = normalizeAssistantNameFromToken(token);
	const matchedByName = assistants.find((assistant) => assistant.name === tokenName);
	if (matchedByName) {
		return { name: matchedByName.name, avatarUrl: matchedByName.avatar || undefined };
	}

	const matchedMemberByName = projectMembers?.find(
		(member) =>
			member.type === "assistant" &&
			!member.isDefault &&
			!isSystemDefaultAssistant(member.publicId) &&
			member.name === tokenName,
	);
	if (matchedMemberByName) {
		return {
			name: matchedMemberByName.name,
			avatarUrl: matchedMemberByName.avatarUrl,
		};
	}

	return { name: tokenName || DEFAULT_ASSISTANT_NAME };
}

function resolveInvokedAssistantProfile(
	message: Message | undefined,
	assistants: DigitalAssistantItem[],
	projectMembers?: ProjectMember[],
): { name: string; avatarUrl?: string } | undefined {
	const invokedAssistant = message?.metadata?.invokedAssistant;
	if (!invokedAssistant?.name?.trim()) return undefined;

	// 中文注释：实际发送内容剥离 @队友时，用 metadata 中的召唤队友信息兜底恢复头像和名称。
	const token: ComposerToken = {
		kind: "assistant",
		label: `@${invokedAssistant.name}`,
		start: 0,
		end: invokedAssistant.name.length + 1,
	};
	if (invokedAssistant.id) token.id = invokedAssistant.id;
	const profile = resolveAssistantProfile(token, assistants, projectMembers);
	const result: { name: string; avatarUrl?: string } = {
		name: profile.name,
	};
	const avatarUrl = profile.avatarUrl ?? invokedAssistant.avatarUrl;
	if (avatarUrl) result.avatarUrl = avatarUrl;
	return result;
}

function resolveTriggeringUserMessage(
	message: Message,
	messagesMap: Record<string, Message>,
): Message | undefined {
	const replyTargetId = message.replyTo?.messageId ?? getReplyTargetMessageId(message.runId);
	if (!replyTargetId) return undefined;

	const target = messagesMap[replyTargetId];
	return target?.role === "user" ? target : undefined;
}

/** 根据触发本轮回复的用户消息，解析 assistant 气泡应展示的队友名称与头像。 */
export function resolveAssistantMessageDisplay(params: {
	message: Message;
	messagesMap: Record<string, Message>;
	assistants: DigitalAssistantItem[];
	projectMembers?: ProjectMember[];
}): AssistantMessageDisplay {
	const { message, messagesMap, assistants, projectMembers } = params;
	const triggeringUserMessage = resolveTriggeringUserMessage(message, messagesMap);
	const assistantToken =
		findAssistantComposerToken(triggeringUserMessage) ?? findAssistantComposerToken(message);
	const invokedProfile =
		resolveInvokedAssistantProfile(triggeringUserMessage, assistants, projectMembers) ??
		resolveInvokedAssistantProfile(message, assistants, projectMembers);

	// 中文注释：优先使用显式 @token；若 token 仅作为展示元信息保存，则使用 invokedAssistant 兜底。
	if (!assistantToken) {
		if (invokedProfile) {
			return {
				useDefaultBrand: false,
				name: invokedProfile.name,
				avatarUrl: invokedProfile.avatarUrl,
			};
		}
		return { useDefaultBrand: true, name: DEFAULT_ASSISTANT_NAME };
	}

	const profile = resolveAssistantProfile(assistantToken, assistants, projectMembers);
	return {
		useDefaultBrand: false,
		name: profile.name,
		avatarUrl: profile.avatarUrl,
	};
}
