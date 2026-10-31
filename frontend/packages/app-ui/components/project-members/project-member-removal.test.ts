import type { ProjectMember } from "@lework/store";
import { describe, expect, it } from "vitest";

import { canQuickRemoveProjectMember } from "./project-member-removal";

function createMember(overrides: Partial<ProjectMember> = {}): ProjectMember {
	return {
		id: "user-member",
		memberId: 1,
		publicId: "member-1",
		type: "user",
		role: "member",
		name: "测试成员",
		...overrides,
	};
}

describe("canQuickRemoveProjectMember", () => {
	it("登录用户身份未就绪时关闭所有快速删除入口", () => {
		expect(canQuickRemoveProjectMember(createMember(), undefined)).toBe(false);
	});

	it("项目创建者不可快速删除", () => {
		expect(canQuickRemoveProjectMember(createMember({ role: "owner" }), "current-user")).toBe(
			false,
		);
	});

	it("当前登录成员不可通过普通删除入口退出项目", () => {
		expect(
			canQuickRemoveProjectMember(createMember({ publicId: "current-user" }), "current-user"),
		).toBe(false);
	});

	it("其他真人成员可以快速删除", () => {
		expect(canQuickRemoveProjectMember(createMember(), "current-user")).toBe(true);
	});

	it("AI 队友可以快速删除", () => {
		expect(
			canQuickRemoveProjectMember(
				createMember({
					id: "assistant-1",
					publicId: "assistant-1",
					type: "assistant",
				}),
				"current-user",
			),
		).toBe(true);
	});
});
