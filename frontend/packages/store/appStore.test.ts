import { describe, expect, it } from "vitest";

import { useAppStore } from "./appStore";

describe("useAppStore.resetAuthScopedData", () => {
	it("同时清空 projects 与 assistants，不被 slice 同名方法覆盖", () => {
		useAppStore.setState({
			projects: [
				{
					id: "project-a",
					name: "账号 A 项目",
					description: "",
					skills: [],
					members: [],
					taskCount: 0,
					createdAt: 1,
					updatedAt: 1,
					messages: [],
					tasks: [],
					files: [],
				},
			],
			assistants: [
				{
					id: 1,
					publicId: "da-a",
					name: "队友 A",
					roleName: "",
					description: "",
					avatar: "",
					status: "active",
					systemPrompt: "",
					expertise: [],
					source: "",
					deploymentPublicId: "",
					deploymentStatus: "",
					deploymentError: "",
					version: 1,
					createdAt: 1,
					updatedAt: 1,
				},
			],
			assistantsLoaded: true,
		});

		useAppStore.getState().resetAuthScopedData();

		const state = useAppStore.getState();
		expect(state.projects).toEqual([]);
		expect(state.assistants).toEqual([]);
		expect(state.assistantsLoaded).toBe(false);
	});
});
