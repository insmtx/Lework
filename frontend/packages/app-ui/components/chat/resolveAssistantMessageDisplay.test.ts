import type { DigitalAssistantItem, ProjectMember } from "@leros/store";
import type { Message } from "@leros/store/types/chat";
import { describe, expect, it } from "vitest";

import { resolveAssistantMessageDisplay } from "./resolveAssistantMessageDisplay";

const assistants: DigitalAssistantItem[] = [
	{
		id: 12,
		publicId: "assistant_alpha",
		name: "投标策略师",
		roleName: "投标经理",
		description: "",
		avatar: "file_avatar_alpha",
		status: "active",
		systemPrompt: "",
		expertise: [],
		source: "",
		deploymentPublicId: "",
		deploymentStatus: "ready",
		deploymentError: "",
		version: 1,
		createdAt: 1,
		updatedAt: 1,
	},
];

const projectMembers: ProjectMember[] = [
	{
		id: "member-1",
		memberId: 12,
		publicId: "assistant_alpha",
		type: "assistant",
		role: "member",
		name: "投标策略师",
		avatarUrl: "file_avatar_alpha",
	},
];

function userMessage(id: string, metadata?: Message["metadata"]): Message {
	return {
		id,
		conversationId: "sess_1",
		role: "user",
		content: "@投标策略师 你好",
		timestamp: 1,
		metadata,
	};
}

function assistantMessage(overrides: Partial<Message> = {}): Message {
	return {
		id: "msg-assistant-req_101",
		conversationId: "sess_1",
		role: "assistant",
		content: "你好",
		timestamp: 2,
		runId: "req_101",
		replyTo: {
			messageId: "101",
			authorName: "张三",
			content: "@投标策略师 你好",
		},
		author: {
			id: "12",
			name: "lework",
			type: "assistant",
		},
		...overrides,
	};
}

describe("resolveAssistantMessageDisplay", () => {
	it("uses default brand when user did not mention an assistant", () => {
		const messagesMap = {
			"101": userMessage("101"),
		};

		const display = resolveAssistantMessageDisplay({
			message: assistantMessage(),
			messagesMap,
			assistants,
			projectMembers,
		});

		expect(display).toEqual({
			useDefaultBrand: true,
			name: "Lework",
		});
	});

	it("shows the selected assistant when composer token exists on the triggering user message", () => {
		const messagesMap = {
			"101": userMessage("101", {
				composerTokens: [
					{
						kind: "assistant",
						id: "assistant_alpha",
						label: "@投标策略师",
						start: 0,
						end: 6,
					},
				],
			}),
		};

		const display = resolveAssistantMessageDisplay({
			message: assistantMessage(),
			messagesMap,
			assistants,
			projectMembers,
		});

		expect(display).toEqual({
			useDefaultBrand: false,
			name: "投标策略师",
			roleName: "投标经理",
			avatarUrl: "file_avatar_alpha",
		});
	});

	it("uses assistant placeholder metadata before the user echo arrives", () => {
		const display = resolveAssistantMessageDisplay({
			message: assistantMessage({
				replyTo: undefined,
				runId: undefined,
				metadata: {
					composerTokens: [
						{
							kind: "assistant",
							id: "assistant_alpha",
							label: "@投标策略师",
							start: 0,
							end: 6,
						},
					],
				},
			}),
			messagesMap: {},
			assistants,
			projectMembers,
		});

		expect(display).toEqual({
			useDefaultBrand: false,
			name: "投标策略师",
			roleName: "投标经理",
			avatarUrl: "file_avatar_alpha",
		});
	});

	it("uses invoked assistant metadata when the visible mention was stripped from content", () => {
		const assistant = assistants[0];
		if (!assistant) throw new Error("missing test assistant");

		const messagesMap = {
			"101": userMessage("101", {
				displayContent: `@${assistant.name} 浣犲ソ`,
				invokedAssistant: {
					id: assistant.publicId,
					name: assistant.name,
					avatarUrl: assistant.avatar,
				},
			}),
		};

		const display = resolveAssistantMessageDisplay({
			message: assistantMessage(),
			messagesMap,
			assistants,
			projectMembers,
		});

		expect(display).toEqual({
			useDefaultBrand: false,
			name: assistant.name,
			roleName: assistant.roleName,
			avatarUrl: assistant.avatar,
		});
	});
});
