import type { ComposerToken } from "@leros/store/types/chat";
import { buildDefaultSummonPrompt } from "../digitalAssistant/promptSuggestions";

export function buildSkillWorkbenchPrefill(
	code: string,
	prompt?: string,
	displayName?: string,
): {
	value: string;
	tokens: ComposerToken[];
} {
	const token = `/${displayName?.trim() || code}`;
	const promptSuffix = prompt ? `${prompt}` : "";
	return {
		value: `${token} ${promptSuffix}`,
		tokens: [
			{
				kind: "skill",
				id: code,
				label: token,
				start: 0,
				end: token.length,
			},
		],
	};
}

export function buildAssistantWorkbenchPrefill(
	assistantIdentity: string,
	assistant: { name: string; expertise: string[]; source?: string },
	prompt?: string,
): {
	value: string;
	tokens: ComposerToken[];
} {
	const mention = `@${assistant.name}`;
	const content = prompt?.trim() || buildDefaultSummonPrompt(assistant);
	const value = `${mention} ${content}`;

	return {
		value,
		tokens: [
			{
				kind: "assistant",
				id: assistantIdentity,
				label: mention,
				start: 0,
				end: mention.length,
			},
		],
	};
}
