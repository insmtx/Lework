import { afterEach, describe, expect, it, vi } from "vitest";

import { projectApi } from "../api/projectApi";
import type { Project, ProjectMember } from "./layoutSlice";
import { LayoutActionImpl, mergeProjectsFromListResult } from "./layoutSlice";

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
		members: overrides.members ?? [],
		taskCount: overrides.taskCount ?? 0,
		createdAt: overrides.createdAt ?? 0,
		updatedAt: overrides.updatedAt,
		messages: overrides.messages ?? [],
		tasks: overrides.tasks ?? [],
		files: overrides.files ?? [],
	};
}

afterEach(() => {
	vi.restoreAllMocks();
});

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

describe("LayoutActionImpl composer draft reset", () => {
	it("从任务详情切回项目页时会清空输入草稿", () => {
		const clearComposerInput = vi.fn();
		const setState = vi.fn();
		const getState = () =>
			({
				currentView: "taskDetail",
				activeProjectId: "project-1",
				activeTaskDetailProjectId: "project-1",
				activeTaskDetailTaskId: "task-1",
				activeTaskDetailSessionId: "session-1",
				clearComposerInput,
			}) as never;

		const actions = new LayoutActionImpl(setState, getState);
		actions.switchProject("project-1");

		expect(clearComposerInput).toHaveBeenCalledTimes(1);
		expect(setState).toHaveBeenCalledTimes(1);
	});

	it("切回创建任务首页时会清空输入草稿", () => {
		const clearComposerInput = vi.fn();
		const setState = vi.fn();
		const getState = () =>
			({
				currentView: "project",
				activeProjectId: "project-1",
				clearComposerInput,
			}) as never;

		const actions = new LayoutActionImpl(setState, getState);
		actions.switchView("workbench");

		expect(clearComposerInput).toHaveBeenCalledTimes(1);
		expect(setState).toHaveBeenCalledTimes(1);
	});
});

describe("LayoutActionImpl.updateProjectMembers", () => {
	it("快速删除成员只更新一次项目并直接写入本地成员快照", async () => {
		const originalProject = createProject({
			id: "project-1",
			name: "测试项目",
			updatedAt: 10,
			members: [
				{
					id: "user-owner",
					memberId: 1,
					publicId: "owner",
					type: "user",
					role: "owner",
					name: "创建者",
				},
				{
					id: "user-member",
					memberId: 2,
					publicId: "member",
					type: "user",
					role: "member",
					name: "普通成员",
				},
			],
		});
		const localMembers: ProjectMember[] = [originalProject.members[0] as ProjectMember];
		const invalidate = vi.fn();
		let state = { projects: [originalProject], invalidate };
		const setState = (partial: unknown) => {
			const update =
				typeof partial === "function"
					? (partial as (current: typeof state) => Partial<typeof state>)(state)
					: (partial as Partial<typeof state>);
			state = { ...state, ...update };
		};
		const update = vi.spyOn(projectApi, "update").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					public_id: "project-1",
					name: "测试项目",
					created_at: "2026-07-16T00:00:00Z",
					updated_at: "2026-07-16T00:00:01Z",
				},
			},
		} as never);
		const actions = new LayoutActionImpl(setState as never, (() => state) as never);

		const result = await actions.updateProjectMembers(
			{
				public_id: "project-1",
				members: [{ type: "user", id: "owner", role: "owner" }],
			},
			localMembers,
		);

		expect(update).toHaveBeenCalledTimes(1);
		expect(state.projects[0]?.members).toEqual(localMembers);
		expect(result?.members).toEqual(localMembers);
		expect(invalidate).not.toHaveBeenCalled();
	});
});

describe("LayoutActionImpl.sendWorkbenchMessage", () => {
	it("续聊已有任务时先拉历史再走任务群聊发送，不再 bootstrap 覆盖历史", async () => {
		const setActiveSession = vi.fn();
		const loadConversationMessages = vi.fn().mockResolvedValue(undefined);
		const sendTaskRoomMessage = vi.fn().mockResolvedValue({
			project_id: "project-1",
			task_id: "task-1",
			session_id: "session-1",
		});
		const bootstrapNewTaskSession = vi.fn();
		const state = {
			activeWorkbenchProjectId: "project-1",
			activeWorkbenchTaskId: "task-1",
			projects: [
				createProject({
					id: "project-1",
					name: "项目 1",
					updatedAt: 1,
					tasks: [
						{
							id: "task-1",
							title: "任务 1",
							meta: "",
							status: "todo",
							sessionId: "session-1",
						},
					],
				}),
			],
			setActiveSession,
			loadConversationMessages,
			sendTaskRoomMessage,
			bootstrapNewTaskSession,
		};
		const setState = vi.fn();
		const actions = new LayoutActionImpl(setState as never, (() => state) as never);

		const result = await actions.sendWorkbenchMessage("继续提问", "project-1");

		expect(result).toEqual({
			project_id: "project-1",
			task_id: "task-1",
			session_id: "session-1",
		});
		expect(setActiveSession).toHaveBeenCalledWith("session-1");
		expect(loadConversationMessages).toHaveBeenCalledWith("session-1", { resumeStream: false });
		expect(sendTaskRoomMessage).toHaveBeenCalledWith(
			"继续提问",
			{
				projectId: "project-1",
				taskId: "task-1",
				sessionId: "session-1",
				metadata: undefined,
			},
			undefined,
		);
		expect(bootstrapNewTaskSession).not.toHaveBeenCalled();
		expect(loadConversationMessages).toHaveBeenCalledBefore(sendTaskRoomMessage);
	});
});
