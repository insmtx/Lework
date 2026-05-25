import { projectApi } from "../api/projectApi";
import { sessionApi } from "../api/sessionApi";
import { taskApi } from "../api/taskApi";
import type { BackendProject, BackendSession, BackendTask } from "../api/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";

export type WorkspaceMode = "remote" | "local";

export type Conversation = {
	id: string;
	title: string;
	type: string;
	status: string;
	createdAt: number;
	updatedAt: number;
};

export type Workspace = {
	id: string;
	name: string;
	mode: WorkspaceMode;
	collapsed: boolean;
};

export type ProjectMessage = {
	id: string;
	role: "assistant" | "user";
	content: string;
	timestamp: number;
};

export type ProjectTaskStatus = "todo" | "in_progress" | "done";

export type ProjectTask = {
	id: string;
	title: string;
	meta: string;
	status: ProjectTaskStatus;
};

export type ProjectArtifact = {
	id: string;
	name: string;
	type: "document" | "spreadsheet" | "image";
	size: string;
	updatedAt: string;
};

export type ProjectMemory = {
	id: string;
	title: string;
	content: string;
};

export type Project = {
	id: string;
	name: string;
	description: string;
	updatedAt: number;
	_backendId?: number;
	messages: ProjectMessage[];
	tasks: ProjectTask[];
	artifacts: ProjectArtifact[];
	files: ProjectArtifact[];
	memories: ProjectMemory[];
};

export type NavGroup = {
	id: string;
	label: string;
	items: NavItem[];
};

export type NavItem = {
	id: string;
	label: string;
	icon: string;
	badge?: number;
};

export type ViewMode =
	| "chat"
	| "workbench"
	| "tasks"
	| "project"
	| "digitalAssistant"
	| "knowledge"
	| "skills"
	| "settings";

export type LayoutState = {
	leftRailCollapsed: boolean;
	rightRailCollapsed: boolean;
	conversationListOpen: boolean;
	currentView: ViewMode;
	activeConversationId: string | null;
	activeWorkspaceId: string | null;
	activeProjectId: string | null;
	activeWorkbenchTaskId: string | null;
	activeProjectTab: "chat" | "tasks" | "files" | "memory";
	workspaces: Workspace[];
	projects: Project[];
	conversations: Conversation[];
	conversationsLoaded: boolean;
	inputFocused: boolean;
	activeRightTab: "shortcuts" | "inbox" | "artifacts";
	navGroups: NavGroup[];
	collapsedNavGroups: Set<string>;
	conversationSearchQuery: string;
};

export type LayoutAction = Pick<LayoutActionImpl, keyof LayoutActionImpl>;
export type LayoutStore = LayoutState & LayoutAction;

function mapSessionToConversation(s: BackendSession): Conversation {
	return {
		id: s.session_id,
		title: s.title || "未命名会话",
		type: s.type,
		status: s.status,
		createdAt: new Date(s.created_at).getTime(),
		updatedAt: new Date(s.updated_at).getTime(),
	};
}

function mapBackendProject(bp: BackendProject): Project {
	return {
		id: bp.public_id,
		_backendId: bp.id,
		name: bp.name,
		description: bp.description ?? "",
		updatedAt: new Date(bp.updated_at).getTime(),
		messages: [],
		tasks: [],
		artifacts: [],
		files: [],
		memories: [],
	};
}

function mapBackendTask(bt: BackendTask): ProjectTask {
	return {
		id: bt.public_id,
		title: bt.title,
		meta: bt.description ?? bt.task_type ?? "",
		status: (bt.status as ProjectTaskStatus) ?? "todo",
	};
}

const _initialState: LayoutState = {
	leftRailCollapsed: false,
	rightRailCollapsed: false,
	conversationListOpen: true,
	currentView: "workbench",
	activeConversationId: null,
	activeWorkspaceId: null,
	activeProjectId: null,
	activeWorkbenchTaskId: null,
	activeProjectTab: "chat",
	workspaces: [
		{ id: "remote-1", name: "远程工作区", mode: "remote", collapsed: false },
		{ id: "local-1", name: "本地工作区", mode: "local", collapsed: false },
	],
	projects: [],
	conversations: [],
	conversationsLoaded: false,
	inputFocused: false,
	activeRightTab: "shortcuts",
	navGroups: [
		{
			id: "core",
			label: "",
			items: [
				{ id: "workbench", label: "工作台", icon: "IconWorkbench" },
				{ id: "tasks", label: "任务", icon: "IconTask" },
				{ id: "skills", label: "技能", icon: "IconSkill" },
				{ id: "knowledge", label: "知识库", icon: "IconKnowledge" },
			],
		},
		{
			id: "projects",
			label: "项目",
			items: [],
		},
		{
			id: "ai-teammates",
			label: "AI 队友",
			items: [
				{ id: "ai-1", label: "Ada AI", icon: "IconAITeammate", badge: 1 },
				{ id: "ai-2", label: "Hopper", icon: "IconAITeammate" },
				{ id: "ai-3", label: "Mia", icon: "IconAITeammate" },
			],
		},
	],
	collapsedNavGroups: new Set(),
	conversationSearchQuery: "",
};

type SetState = (
	partial:
		| LayoutStore
		| Partial<LayoutStore>
		| ((state: LayoutStore) => LayoutStore | Partial<LayoutStore>),
	replace?: boolean,
) => void;

export const createLayoutSlice = (set: SetState, get: () => LayoutStore) =>
	new LayoutActionImpl(set, get);

export class LayoutActionImpl {
	readonly #set: SetState;
	readonly #get: () => LayoutStore;

	constructor(set: SetState, get: () => LayoutStore) {
		this.#set = set;
		this.#get = get;
	}

	toggleLeftRail = () => {
		this.#set((state) => ({ leftRailCollapsed: !state.leftRailCollapsed }));
	};

	toggleConversationList = () => {
		this.#set((state) => ({
			conversationListOpen: !state.conversationListOpen,
		}));
	};

	switchView = (view: ViewMode) => {
		this.#set({
			currentView: view,
			conversationListOpen: view === "chat",
		});
	};

	switchProject = (projectId: string) => {
		this.#set({
			activeProjectId: projectId,
			activeProjectTab: "chat",
			currentView: "project",
			conversationListOpen: false,
		});
	};

	selectWorkbenchProject = (projectId: string | null) => {
		this.#set({ activeProjectId: projectId, activeWorkbenchTaskId: null });
	};

	selectWorkbenchTask = (taskId: string | null) => {
		this.#set({ activeWorkbenchTaskId: taskId });
	};

	setActiveProjectTab = (tab: "chat" | "tasks" | "files" | "memory") => {
		this.#set({ activeProjectTab: tab });
	};

	sendWorkbenchMessage = (content: string, projectId?: string | null) => {
		const trimmed = content.trim();
		if (!trimmed) return;

		const state = this.#get();
		const targetProject = projectId
			? state.projects.find((project) => project.id === projectId)
			: null;
		const targetProjectId = targetProject?.id ?? `project-${Date.now()}`;
		const projectName =
			targetProject?.name ?? createProjectName(trimmed, state.projects.length + 1);
		const timestamp = Date.now();
		const userMessage: ProjectMessage = {
			id: `${targetProjectId}-user-${timestamp}`,
			role: "user",
			content: trimmed,
			timestamp,
		};
		const assistantMessage: ProjectMessage = {
			id: `${targetProjectId}-assistant-${timestamp}`,
			role: "assistant",
			content: `已收到，我会围绕「${projectName}」拆解任务、同步上下文，并把后续产物沉淀到项目中。`,
			timestamp: timestamp + 1,
		};
		const localTaskId = `${targetProjectId}-task-${timestamp}`;
		const nextTask: ProjectTask = {
			id: localTaskId,
			title: createTaskTitle(trimmed),
			meta: "由工作台消息生成 · 待处理",
			status: "todo",
		};

		this.#set((state) => {
			if (targetProject) {
				return {
					activeProjectId: targetProjectId,
					projects: state.projects.map((project) =>
						project.id === targetProjectId
							? {
									...project,
									updatedAt: timestamp,
									messages: [...project.messages, userMessage, assistantMessage],
									tasks: [nextTask, ...project.tasks],
								}
							: project,
					),
				};
			}

			const newProject: Project = {
				id: targetProjectId,
				name: projectName,
				description: "由工作台消息自动创建",
				updatedAt: timestamp,
				messages: [userMessage, assistantMessage],
				tasks: [nextTask],
				artifacts: [
					{
						id: `${targetProjectId}-artifact-${timestamp}`,
						name: "project-brief.md",
						type: "document",
						size: "2.4 KB",
						updatedAt: "刚刚",
					},
				],
				files: [],
				memories: [
					{
						id: `${targetProjectId}-memory-${timestamp}`,
						title: "初始需求",
						content: trimmed,
					},
				],
			};

			return {
				activeProjectId: targetProjectId,
				projects: [newProject, ...state.projects],
			};
		});

		this.#persistTask(targetProject, targetProjectId, localTaskId, trimmed);
	};

	#persistTask = async (
		targetProject: Project | null,
		targetProjectId: string,
		localTaskId: string,
		trimmed: string,
	) => {
		try {
			let backendProjectId: number | null = targetProject?._backendId ?? null;
			let publicId: string | null = null;

			if (!backendProjectId) {
				const name = targetProject?.name ?? createProjectName(trimmed, this.#get().projects.length + 1);
				const projectRes = await projectApi.create({ name });
				const bp = projectRes.data.data;
				if (!bp) throw new Error("Failed to create project");
				backendProjectId = bp.id;
				publicId = bp.public_id;
			}

			const taskRes = await taskApi.create({
				project_id: backendProjectId,
				title: createTaskTitle(trimmed),
			});
			const bt = taskRes.data.data;
			if (!bt) throw new Error("Failed to create task");
			const backendTask = mapBackendTask(bt);

			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === targetProjectId
						? {
								...p,
								...(publicId ? { id: publicId, _backendId: backendProjectId } : {}),
								tasks: p.tasks.map((t) =>
									t.id === localTaskId ? backendTask : t,
								),
							}
						: p,
				),
				activeProjectId: publicId ?? s.activeProjectId,
			}));
		} catch (err) {
			console.error("persistTask error:", err);
		}
	};

	fetchProjects = async () => {
		const state = this.#get();
		if (state.projects.length > 0) return;
		try {
			const res = await projectApi.list({ list_all: true, limit: 100 });
			const items = res.data.data?.items ?? [];
			this.#set({
				projects: items.map(mapBackendProject),
			});
		} catch (err) {
			console.error("fetchProjects error:", err);
		}
	};

	createProject = async (params: { name: string; description?: string; metadata?: Record<string, unknown> }) => {
		try {
			const res = await projectApi.create(params);
			const bp = res.data.data;
			if (!bp) throw new Error("No data returned");
			const item = mapBackendProject(bp);
			this.#set((state) => ({
				projects: [item, ...state.projects],
			}));
			return item;
		} catch (err) {
			console.error("createProject error:", err);
			return null;
		}
	};

	updateProject = async (params: {
		public_id: string;
		name?: string;
		description?: string;
		status?: string;
		owner_id?: number;
		metadata?: Record<string, unknown>;
	}) => {
		try {
			const res = await projectApi.update(params);
			const bp = res.data.data;
			if (!bp) throw new Error("No data returned");
			const item = mapBackendProject(bp);
			this.#set((state) => ({
				projects: state.projects.map((p) => (p.id === item.id ? { ...p, ...item } : p)),
			}));
			return item;
		} catch (err) {
			console.error("updateProject error:", err);
			return null;
		}
	};

	deleteProject = async (publicId: string) => {
		try {
			await projectApi.delete({ public_id: publicId });
			this.#set((state) => ({
				projects: state.projects.filter((p) => p.id !== publicId),
				activeProjectId: state.activeProjectId === publicId ? null : state.activeProjectId,
			}));
		} catch (err) {
			console.error("deleteProject error:", err);
		}
	};

	fetchTasks = async (projectId: string) => {
		const state = this.#get();
		const project = state.projects.find((p) => p.id === projectId);
		if (!project || !project._backendId) return;

		try {
			const res = await taskApi.list({ project_id: project._backendId, list_all: true, limit: 100 });
			const items = res.data.data?.items ?? [];
			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === projectId
						? { ...p, tasks: items.map(mapBackendTask) }
						: p,
				),
			}));
		} catch (err) {
			console.error("fetchTasks error:", err);
		}
	};

	createTask = async (projectId: string, params: {
		title: string;
		description?: string;
		assignee_id?: number;
		task_type?: string;
		deadline?: string;
		metadata?: Record<string, unknown>;
	}) => {
		const state = this.#get();
		const project = state.projects.find((p) => p.id === projectId);
		if (!project || !project._backendId) return null;

		try {
			const res = await taskApi.create({ project_id: project._backendId, ...params });
			const bt = res.data.data;
			if (!bt) throw new Error("No data returned");
			const item = mapBackendTask(bt);
			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === projectId
						? { ...p, tasks: [item, ...p.tasks], updatedAt: Date.now() }
						: p,
				),
			}));
			return item;
		} catch (err) {
			console.error("createTask error:", err);
			return null;
		}
	};

	updateTask = async (params: {
		public_id: string;
		title?: string;
		description?: string;
		status?: string;
		assignee_id?: number;
		task_type?: string;
		deadline?: string;
		metadata?: Record<string, unknown>;
	}) => {
		try {
			const res = await taskApi.update(params);
			const bt = res.data.data;
			if (!bt) throw new Error("No data returned");
			const item = mapBackendTask(bt);
			this.#set((s) => ({
				projects: s.projects.map((p) => ({
					...p,
					tasks: p.tasks.map((t) => (t.id === item.id ? item : t)),
				})),
			}));
			return item;
		} catch (err) {
			console.error("updateTask error:", err);
			return null;
		}
	};

	deleteTask = async (publicId: string) => {
		try {
			await taskApi.delete({ public_id: publicId });
			this.#set((s) => ({
				projects: s.projects.map((p) => ({
					...p,
					tasks: p.tasks.filter((t) => t.id !== publicId),
				})),
				activeWorkbenchTaskId:
					this.#get().activeWorkbenchTaskId === publicId ? null : this.#get().activeWorkbenchTaskId,
			}));
		} catch (err) {
			console.error("deleteTask error:", err);
		}
	};

	toggleRightRail = () => {
		this.#set((state) => ({ rightRailCollapsed: !state.rightRailCollapsed }));
	};

	toggleWorkspaceCollapse = (workspaceId: string) => {
		this.#set((state) => ({
			workspaces: state.workspaces.map((w) =>
				w.id === workspaceId ? { ...w, collapsed: !w.collapsed } : w,
			),
		}));
	};

	switchConversation = (conversationId: string) => {
		this.#set({ activeConversationId: conversationId });
	};

	fetchConversations = async () => {
		if (this.#get().conversationsLoaded) return;
		try {
			const res = await sessionApi.list({ page: 1, per_page: 50 });
			const items = res.data.data?.items ?? [];
			this.#set({
				conversations: items.map(mapSessionToConversation),
				conversationsLoaded: true,
			});
		} catch (err) {
			console.error("fetchConversations error:", err);
		}
	};

	createConversation = async (title: string) => {
		try {
			const res = await sessionApi.create({
				type: "chat",
				title: title || "新会话",
			});
			const session = res.data.data;
			if (!session) throw new Error("No session data returned");
			const conv = mapSessionToConversation(session);
			this.#set((state) => ({
				conversations: [conv, ...state.conversations],
				activeConversationId: conv.id,
				conversationsLoaded: true,
			}));
			return conv;
		} catch (err) {
			console.error("createConversation error:", err);
			return null;
		}
	};

	deleteConversation = async (conversationId: string) => {
		const state = this.#get();
		const conv = state.conversations.find((c) => c.id === conversationId);
		if (!conv) return;

		try {
			await sessionApi.delete(conv.id);
			this.#set((state) => ({
				conversations: state.conversations.filter((c) => c.id !== conversationId),
				activeConversationId:
					state.activeConversationId === conversationId ? null : state.activeConversationId,
			}));
		} catch (err) {
			console.error("deleteConversation error:", err);
		}
	};

	updateConversationTitle = async (conversationId: string, title: string) => {
		const state = this.#get();
		const conv = state.conversations.find((c) => c.id === conversationId);
		if (!conv) return;

		try {
			await sessionApi.update({ session_id: conv.id, title });
			this.#set((state) => ({
				conversations: state.conversations.map((c) =>
					c.id === conversationId ? { ...c, title, updatedAt: Date.now() } : c,
				),
			}));
		} catch (err) {
			console.error("updateConversationTitle error:", err);
		}
	};

	setInputFocused = (focused: boolean) => {
		this.#set({ inputFocused: focused });
	};

	setActiveRightTab = (tab: "shortcuts" | "inbox" | "artifacts") => {
		this.#set({ activeRightTab: tab });
	};

	toggleNavGroup = (groupId: string) => {
		this.#set((state) => {
			const collapsed = new Set(state.collapsedNavGroups);
			if (collapsed.has(groupId)) {
				collapsed.delete(groupId);
			} else {
				collapsed.add(groupId);
			}
			return { collapsedNavGroups: collapsed };
		});
	};

	setConversationSearchQuery = (query: string) => {
		this.#set({ conversationSearchQuery: query });
	};
}

function createProjectName(content: string, index: number): string {
	const firstLine = content.split(/\n/)[0]?.trim();
	if (!firstLine) return `project-${index}`;
	return firstLine.length > 18 ? firstLine.slice(0, 18) : firstLine;
}

function createTaskTitle(content: string): string {
	const firstLine = content.split(/\n/)[0]?.trim();
	if (!firstLine) return "跟进工作台请求";
	return firstLine.length > 24 ? `${firstLine.slice(0, 24)}...` : firstLine;
}

export const layoutSlice: SliceCreator<LayoutStore> = (...params) => ({
	..._initialState,
	...flattenActions<LayoutAction>([createLayoutSlice(params[0] as SetState, params[1])]),
});
