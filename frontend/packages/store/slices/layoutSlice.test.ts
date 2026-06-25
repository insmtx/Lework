import { describe, expect, it } from "vitest";

import type { Project } from "./layoutSlice";
import { mergeProjectsFromListResult } from "./layoutSlice";

function createProject(
	overrides: Partial<Project> & Pick<Project, "id" | "name" | "updatedAt">,
): Project {
	return {
		id: overrides.id,
		name: overrides.name,
		description: overrides.description ?? "",
		objective: overrides.objective,
		metadata: overrides.metadata,
		skills: overrides.skills ?? [],
		createdAt: overrides.createdAt ?? 0,
		updatedAt: overrides.updatedAt,
		messages: overrides.messages ?? [],
		tasks: overrides.tasks ?? [],
		artifacts: overrides.artifacts ?? [],
		files: overrides.files ?? [],
	};
}

describe("mergeProjectsFromListResult", () => {
	it("会保留本地已加载的任务和详情字段，避免列表刷新清空侧栏任务", () => {
		const localProjects = [
			createProject({
				id: "project-1",
				name: "旧项目",
				updatedAt: 10,
				objective: "旧目标",
				tasks: [
					{
						id: "task-1",
						title: "任务 1",
						meta: "",
						status: "todo",
					},
				],
			}),
		];
		const apiProjects = [
			createProject({
				id: "project-1",
				name: "新项目",
				updatedAt: 20,
				tasks: [],
			}),
		];

		const mergedProjects = mergeProjectsFromListResult(apiProjects, localProjects);

		expect(mergedProjects).toHaveLength(1);
		expect(mergedProjects[0]?.name).toBe("新项目");
		expect(mergedProjects[0]?.updatedAt).toBe(20);
		expect(mergedProjects[0]?.objective).toBe("旧目标");
		expect(mergedProjects[0]?.tasks.map((task) => task.id)).toEqual(["task-1"]);
	});

	it("不会保留列表接口中已不存在的本地项目，避免已删除项目残留", () => {
		const localProjects = [
			createProject({
				id: "project-local",
				name: "本地项目",
				updatedAt: 5,
			}),
		];
		const apiProjects = [
			createProject({
				id: "project-1",
				name: "远端项目",
				updatedAt: 20,
			}),
		];

		const mergedProjects = mergeProjectsFromListResult(apiProjects, localProjects);

		expect(mergedProjects.map((project) => project.id)).toEqual(["project-1"]);
	});
});
