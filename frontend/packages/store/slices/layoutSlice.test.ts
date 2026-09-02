import { afterEach, describe, expect, it, vi } from "vitest";

import { projectApi } from "../api/projectApi";
import * as authStorage from "../utils/authStorage";
import type { Project, ProjectMember } from "./layoutSlice";
import {
	appendProjectsFromListResult,
	LayoutActionImpl,
	mergeProjectsFromListResult,
	PROJECT_LIST_PAGE_SIZE,
	upsertProjectsIntoCache,
} from "./layoutSlice";

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

describe("appendProjectsFromListResult", () => {
	it("追加分页时保留已加载项目，并跳过重复 id", () => {
		const localProjects = [
			createProject({
				id: "project-1",
				name: "已加载",
				updatedAt: 20,
				tasks: [{ id: "task-1", title: "任务 1", meta: "", status: "todo" }],
			}),
		];
		const apiProjects = [
			createProject({
				id: "project-1",
				name: "重复项",
				updatedAt: 30,
			}),
			createProject({
				id: "project-2",
				name: "下一页",
				updatedAt: 10,
			}),
		];

		const mergedProjects = appendProjectsFromListResult(apiProjects, localProjects);

		expect(mergedProjects.map((project) => project.id)).toEqual(["project-1", "project-2"]);
		expect(mergedProjects[0]?.tasks.map((task) => task.id)).toEqual(["task-1"]);
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

describe("upsertProjectsIntoCache", () => {
	it("写入实体缓存时保留未出现在本页的本地项目", () => {
		const localProjects = [
			createProject({ id: "project-local", name: "本地项目", updatedAt: 5 }),
			createProject({
				id: "project-1",
				name: "旧名称",
				updatedAt: 10,
				tasks: [{ id: "task-1", title: "任务 1", meta: "", status: "todo" }],
			}),
		];
		const incoming = [
			createProject({
				id: "project-1",
				name: "新名称",
				updatedAt: 20,
			}),
		];

		const cached = upsertProjectsIntoCache(incoming, localProjects);

		expect(cached.map((project) => project.id)).toEqual(["project-1", "project-local"]);
		expect(cached[0]?.name).toBe("新名称");
		expect(cached[0]?.tasks.map((task) => task.id)).toEqual(["task-1"]);
	});
});

describe("LayoutActionImpl.fetchProjects", () => {
	it("只请求第一页写入实体缓存，并保留未出现在本页的本地项目", async () => {
		vi.spyOn(authStorage, "readStoredAuthUser").mockReturnValue({ jwtToken: "token" } as never);
		const list = vi.spyOn(projectApi, "list").mockResolvedValue({
			data: {
				code: 0,
				message: "ok",
				data: {
					total: 45,
					items: Array.from({ length: PROJECT_LIST_PAGE_SIZE }, (_, index) => ({
						public_id: `project-${index + 1}`,
						name: `project-${index + 1}`,
						created_at: "2026-01-01T00:00:00Z",
						updated_at: "2026-01-01T00:00:00Z",
					})),
				},
			},
		} as never);
		const state = {
			projects: [createProject({ id: "project-local", name: "本地项目", updatedAt: 1 })],
			projectsMutationEpoch: 0,
		};
		const setState = (partial: unknown) => {
			const update =
				typeof partial === "function"
					? (partial as (current: typeof state) => Partial<typeof state>)(state)
					: (partial as Partial<typeof state>);
			Object.assign(state, update);
		};
		const actions = new LayoutActionImpl(setState as never, (() => state) as never);

		await expect(actions.fetchProjects()).resolves.toBe(true);

		expect(list).toHaveBeenCalledTimes(1);
		expect(list).toHaveBeenCalledWith({ offset: 0, limit: PROJECT_LIST_PAGE_SIZE });
		expect(state.projects).toHaveLength(PROJECT_LIST_PAGE_SIZE + 1);
		expect(state.projects.some((project) => project.id === "project-local")).toBe(true);
	});
});
