import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MySkillsPanel } from "./MySkillsPanel";

const { mockPluginList } = vi.hoisted(() => ({
	mockPluginList: vi.fn(),
}));

vi.mock("@leros/store", () => ({
	pluginApi: {
		list: mockPluginList,
	},
	pluginToSkillCard: (plugin: {
		public_id: string;
		code: string;
		name: string;
		description?: string;
	}) => ({
		source_type: "organization",
		skill_id: plugin.public_id,
		name: plugin.code,
		display_name: plugin.name,
		description: plugin.description ?? "",
		version: "r1",
		author: "组织插件",
		category: "skill",
		tags: [],
		icon: "",
		installs: 0,
		verified: false,
	}),
}));

describe("MySkillsPanel", () => {
	afterEach(cleanup);

	beforeEach(() => {
		vi.clearAllMocks();
		mockPluginList.mockResolvedValue({
			data: {
				data: {
					plugins: [
						{
							public_id: "plugin_custom",
							code: "custom",
							name: "Custom",
							description: "Organization Skill",
						},
					],
				},
			},
		});
	});

	it("requests shared Skills without marketplace lineage for the 组织共享 tab", async () => {
		render(<MySkillsPanel relation="shared" excludeMarketplaceBased />);

		expect(await screen.findByText("Custom")).toBeInTheDocument();
		await waitFor(() =>
			expect(mockPluginList).toHaveBeenCalledWith({
				kind: "skill",
				status: "active",
				relation: "shared",
				exclude_marketplace_based: true,
			}),
		);
	});

	it("uses the owner relation for the 我的 tab", async () => {
		render(<MySkillsPanel relation="owner" emptyMessage="你还没有拥有的技能" />);

		expect(await screen.findByText("Custom")).toBeInTheDocument();
		await waitFor(() =>
			expect(mockPluginList).toHaveBeenCalledWith({
				kind: "skill",
				status: "active",
				relation: "owner",
			}),
		);
	});
});
