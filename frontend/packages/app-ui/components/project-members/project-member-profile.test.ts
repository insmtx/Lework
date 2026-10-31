import type { ProjectMember } from "@lework/store";
import { describe, expect, it } from "vitest";

import { applyCurrentUserProfileToProjectMembers } from "./project-member-profile";

function createMember(overrides: Partial<ProjectMember> = {}): ProjectMember {
	return {
		id: "user-owner",
		memberId: 17,
		publicId: "user-1",
		type: "user",
		role: "owner",
		name: "旧名称",
		avatarUrl: "old-avatar",
		...overrides,
	};
}

describe("applyCurrentUserProfileToProjectMembers", () => {
	it("按 publicId 把当前登录用户的最新名称同步到项目队友列表", () => {
		const members = [
			createMember(),
			createMember({
				id: "user-other",
				memberId: 18,
				publicId: "user-2",
				name: "同事",
			}),
		];

		expect(
			applyCurrentUserProfileToProjectMembers(members, {
				publicId: "user-1",
				uin: 17,
				name: "新名称",
				uinName: "新名称",
			}).map((member) => member.name),
		).toEqual(["新名称", "同事"]);
	});

	it("publicId 缺失时按 uin 识别本人并更新名称", () => {
		const members = [createMember({ publicId: undefined })];

		expect(
			applyCurrentUserProfileToProjectMembers(members, {
				uin: 17,
				name: "新名称",
			})[0]?.name,
		).toBe("新名称");
	});

	it("不改写其他队友或 AI 队友的名称", () => {
		const members = [
			createMember({
				id: "assistant-1",
				memberId: 9,
				publicId: "assistant-1",
				type: "assistant",
				name: "策略师",
			}),
		];

		expect(
			applyCurrentUserProfileToProjectMembers(members, {
				publicId: "user-1",
				uin: 17,
				name: "新名称",
			})[0]?.name,
		).toBe("策略师");
	});
});
