import { describe, expect, it } from "vitest";

import type { ProjectActivityItem } from "@leros/store";

import {
	buildProjectActivityActionParts,
	formatProjectActivityAction,
} from "./project-activity";

function activityItem(
	overrides: Partial<ProjectActivityItem> & Pick<ProjectActivityItem, "action_type" | "payload">,
): ProjectActivityItem {
	return {
		id: 1,
		project_id: "proj-1",
		operator_id: "user-1",
		created_at: "2026-08-27T00:00:00Z",
		...overrides,
	};
}

const emptyPayload = {
	added_skills: [],
	removed_skills: [],
	added_members: [],
	removed_members: [],
	added_ai_teammates: [],
	removed_ai_teammates: [],
};

describe("buildProjectActivityActionParts", () => {
	it("添加/移除 AI 队友时使用带头像的 actor-list，而不是纯名称文本", () => {
		const item = activityItem({
			action_type: "project.participants.changed",
			payload: {
				...emptyPayload,
				added_ai_teammates: [
					{ id: "asst-1", name: "产品助手", avatar_url: "https://cdn.example/a.png" },
				],
				removed_ai_teammates: [{ id: "asst-2", name: "研发助手" }],
			},
		});

		const parts = buildProjectActivityActionParts(item);

		expect(parts).toEqual([
			{ type: "text", text: "添加了 " },
			{
				type: "actor-list",
				label: "AI队友",
				participantType: "assistant",
				actors: [{ id: "asst-1", name: "产品助手", avatar_url: "https://cdn.example/a.png" }],
			},
			{ type: "text", text: "； " },
			{ type: "text", text: "移除了 " },
			{
				type: "actor-list",
				label: "AI队友",
				participantType: "assistant",
				actors: [{ id: "asst-2", name: "研发助手" }],
			},
		]);
		expect(formatProjectActivityAction(item)).toBe("添加了 AI队友 产品助手； 移除了 AI队友 研发助手");
	});

	it("添加真人成员时同样使用 actor-list", () => {
		const item = activityItem({
			action_type: "project.participants.changed",
			payload: {
				...emptyPayload,
				added_members: [{ id: "user-2", name: "张三", avatar_url: "https://cdn.example/u.png" }],
			},
		});

		expect(buildProjectActivityActionParts(item)).toEqual([
			{ type: "text", text: "添加了 " },
			{
				type: "actor-list",
				label: "成员",
				participantType: "user",
				actors: [{ id: "user-2", name: "张三", avatar_url: "https://cdn.example/u.png" }],
			},
		]);
	});
});
