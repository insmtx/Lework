import "@testing-library/jest-dom/vitest";

import type { ProjectMember } from "@lework/store";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import {
	hasMultipleHumanProjectMembers,
	PROJECT_MCP_COLLABORATION_WARNING,
	ProjectMCPCollaborationWarning,
} from "./project-mcp-collaboration-warning";

function member(type: "user" | "assistant", id: number): ProjectMember {
	return {
		id: `${type}-${id}`,
		memberId: id,
		publicId: `${type}-${id}`,
		type,
		role: "member",
		name: `${type}-${id}`,
	};
}

describe("ProjectMCPCollaborationWarning", () => {
	afterEach(cleanup);

	it("只在项目包含至少两名真人成员时启用多人策略", () => {
		expect(hasMultipleHumanProjectMembers([])).toBe(false);
		expect(hasMultipleHumanProjectMembers([member("user", 1), member("assistant", 2)])).toBe(false);
		expect(hasMultipleHumanProjectMembers([member("user", 1), member("user", 2)])).toBe(true);
	});

	it("多人项目显示可聚焦的警告图标和隐私提示", async () => {
		render(<ProjectMCPCollaborationWarning members={[member("user", 1), member("user", 2)]} />);

		const trigger = screen.getByRole("button", { name: PROJECT_MCP_COLLABORATION_WARNING });
		expect(trigger).toBeInTheDocument();
		fireEvent.mouseEnter(trigger);
		expect(await screen.findByText(PROJECT_MCP_COLLABORATION_WARNING)).toBeInTheDocument();
	});

	it("单人项目不显示警告", () => {
		render(<ProjectMCPCollaborationWarning members={[member("user", 1)]} />);
		expect(
			screen.queryByRole("button", { name: PROJECT_MCP_COLLABORATION_WARNING }),
		).not.toBeInTheDocument();
	});
});
