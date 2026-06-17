import { projectApi } from "../api/projectApi";
import { sessionApi } from "../api/sessionApi";
import { taskApi } from "../api/taskApi";
import type { BackendArtifact, BackendProject, BackendSession, BackendTask } from "../api/types";
import { workApi } from "../api/workApi";
import type { SliceCreator } from "../types";
import type { Attachment } from "../types/chat";
import { flattenActions } from "../utils";
import { formatFileSize } from "../utils/format";

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
	sessionId?: string;
	taskType?: string;
	deadline?: string;
	description?: string;
};

export type ProjectArtifact = {
	id: string;
	name: string;
	title: string;
	description?: string;
	type: "document" | "spreadsheet" | "image";
	artifactType: string;
	mimeType?: string;
	size: string;
	updatedAt: string;
	downloadUrl: string;
	sha256?: string;
};

export type Project = {
	id: string;
	name: string;
	description: string;
	objective?: string;
	updatedAt: number;
	messages: ProjectMessage[];
	tasks: ProjectTask[];
	artifacts: ProjectArtifact[];
	files: ProjectArtifact[];
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
	| "taskDetail"
	| "digitalAssistant"
	| "knowledge"
	| "skills"
	| "settings";

export type LayoutState = {
	leftRailCollapsed: boolean;
	leftRailWidth: number;
	rightRailCollapsed: boolean;
	conversationListOpen: boolean;
	currentView: ViewMode;
	activeConversationId: string | null;
	activeWorkspaceId: string | null;
	activeProjectId: string | null;
	activeWorkbenchProjectId: string | null;
	activeWorkbenchTaskId: string | null;
	activeProjectTab: "chat" | "tasks" | "files";
	workspaces: Workspace[];
	projects: Project[];
	conversations: Conversation[];
	conversationsLoaded: boolean;
	inputFocused: boolean;
	activeRightTab: "shortcuts" | "inbox" | "artifacts";
	navGroups: NavGroup[];
	collapsedNavGroups: Set<string>;
	conversationSearchQuery: string;
	activeTaskDetailProjectId: string | null;
	activeTaskDetailTaskId: string | null;
	activeTaskDetailSessionId: string | null;
	projectDetailLoading: boolean;
	projectDetailError: string | null;
	activeProjectSessionId: string | null;
	projectSessionId: string | null;
	projectSessionProjectId: string | null;
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
		name: bp.name,
		description: bp.description ?? "",
		updatedAt: new Date(bp.updated_at).getTime(),
		messages: [],
		tasks: [],
		artifacts: [],
		files: [],
	};
}

function mapBackendTask(bt: BackendTask): ProjectTask {
	const taskWithSession = bt as BackendTask & { session?: BackendSession };
	return {
		id: bt.public_id,
		title: bt.title,
		meta: bt.description ?? bt.task_type ?? "",
		status: (bt.status as ProjectTaskStatus) ?? "todo",
		sessionId: taskWithSession.session?.session_id,
		taskType: bt.task_type,
		deadline: bt.deadline,
		description: bt.description,
	};
}

export function mapBackendArtifactToProjectArtifact(ba: BackendArtifact): ProjectArtifact {
	const artifactTypeMap: Record<string, ProjectArtifact["type"]> = {
		image: "image",
		spreadsheet: "spreadsheet",
	};
	return {
		id: ba.artifact_id,
		name: ba.filename ?? ba.title,
		title: ba.title,
		description: ba.description,
		type: artifactTypeMap[ba.artifact_type] ?? "document",
		artifactType: ba.artifact_type,
		mimeType: ba.mime_type,
		size: formatFileSize(ba.file_size ?? 0),
		updatedAt: "",
		downloadUrl: "",
		sha256: ba.sha256,
	};
}

const _initialState: LayoutState = {
	leftRailCollapsed: false,
	leftRailWidth: 240,
	rightRailCollapsed: false,
	conversationListOpen: true,
	currentView: "workbench",
	activeConversationId: null,
	activeWorkspaceId: null,
	activeProjectId: null,
	activeWorkbenchProjectId: null,
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
				{ id: "skills", label: "技能库", icon: "IconSkill" },
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
	activeTaskDetailProjectId: null,
	activeTaskDetailTaskId: null,
	activeTaskDetailSessionId: null,
	projectDetailLoading: false,
	projectDetailError: null,
	activeProjectSessionId: null,
	projectSessionId: null,
	projectSessionProjectId: null,
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

	setLeftRailCollapsed = (collapsed: boolean) => {
		this.#set({ leftRailCollapsed: collapsed });
	};

	setLeftRailWidth = (width: number) => {
		// 左侧栏宽度仅允许在可读与不挤压主内容的范围内变化
		const nextWidth = Math.min(320, Math.max(220, Math.round(width)));
		this.#set({ leftRailWidth: nextWidth });
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
			...(view !== "taskDetail"
				? {
						activeTaskDetailProjectId: null,
						activeTaskDetailTaskId: null,
						activeTaskDetailSessionId: null,
					}
				: {}),
		});
	};

	switchProject = (projectId: string) => {
		this.#set({
			activeProjectId: projectId,
			activeProjectTab: "chat",
			currentView: "project",
			conversationListOpen: false,
			activeTaskDetailProjectId: null,
			activeTaskDetailTaskId: null,
			activeTaskDetailSessionId: null,
		});
	};

	setProjectRoute = (projectId: string, tab: "chat" | "tasks" | "files" = "chat") => {
		this.#set({
			activeProjectId: projectId,
			activeProjectTab: tab,
			currentView: "project",
			conversationListOpen: false,
			activeTaskDetailProjectId: null,
			activeTaskDetailTaskId: null,
			activeTaskDetailSessionId: null,
		});
	};

	clearTaskDetailRoute = () => {
		this.#set({
			activeTaskDetailProjectId: null,
			activeTaskDetailTaskId: null,
			activeTaskDetailSessionId: null,
		});
	};

	selectWorkbenchProject = (projectId: string | null) => {
		this.#set({ activeWorkbenchProjectId: projectId, activeWorkbenchTaskId: null });
		if (projectId) {
			this.fetchTasks(projectId);
		}
	};

	selectWorkbenchTask = (taskId: string | null) => {
		this.#set({ activeWorkbenchTaskId: taskId });
	};

	setActiveProjectTab = (tab: "chat" | "tasks" | "files") => {
		this.#set({ activeProjectTab: tab });
	};

	sendWorkbenchMessage = async (
		content: string,
		projectId?: string | null,
		attachments?: Attachment[],
	) => {
		const trimmed = content.trim();
		if (!trimmed) return;

		const state = this.#get();
		const selectedTaskId = state.activeWorkbenchTaskId;

		const workbenchProjectId = projectId ?? state.activeWorkbenchProjectId;

		if (workbenchProjectId && selectedTaskId) {
			let project = state.projects.find((p) => p.id === workbenchProjectId);
			let selectedTask = project?.tasks.find((task) => task.id === selectedTaskId);

			if (!selectedTask?.sessionId) {
				try {
					const detailRes = await projectApi.detail({ public_id: workbenchProjectId });
					const detail = detailRes.data.data;
					if (detail) {
						const tasks = (detail.tasks ?? []).map(mapBackendTask);
						this.#set((s) => ({
							projects: s.projects.map((p) =>
								p.id === workbenchProjectId
									? {
											...p,
											name: detail.name,
											description: detail.description ?? "",
											objective: detail.objective,
											updatedAt: new Date(detail.updated_at).getTime(),
											tasks,
											artifacts: [],
											files: [],
										}
									: p,
							),
							projectSessionId: detail.session?.session_id ?? s.projectSessionId,
							projectSessionProjectId: detail.session?.session_id
								? workbenchProjectId
								: s.projectSessionProjectId,
						}));
						project = { ...(project ?? mapBackendProject(detail)), tasks };
						selectedTask = tasks.find((task) => task.id === selectedTaskId);
					}
				} catch (err) {
					console.error("sendWorkbenchMessage refresh project detail error:", err);
				}
			}

			if (selectedTask?.sessionId) {
				try {
					await sessionApi.addMessage({
						session_id: selectedTask.sessionId,
						role: "user",
						content: trimmed,
						message_type: "text",
						attachments: attachments
							?.filter((attachment): attachment is Attachment & { fileUploadId: string } =>
								Boolean(attachment.fileUploadId?.trim()),
							)
							.map((attachment) => ({
								file_upload_id: attachment.fileUploadId.trim(),
								name: attachment.name,
								mime_type:
									attachment.mimeType || attachment.file?.type || "application/octet-stream",
							})),
					});
					const data = {
						project_id: workbenchProjectId,
						task_id: selectedTaskId,
						session_id: selectedTask.sessionId,
					};
					this.#set({
						activeProjectId: data.project_id,
						activeWorkbenchProjectId: null,
						activeWorkbenchTaskId: null,
						activeTaskDetailProjectId: data.project_id,
						activeTaskDetailTaskId: data.task_id,
						activeTaskDetailSessionId: data.session_id,
						currentView: "taskDetail",
						conversationListOpen: false,
					});
					return data;
				} catch (err) {
					console.error("sendWorkbenchMessage addMessage error:", err);
					return null;
				}
			}
		}

		const params: {
			content: string;
			project_id?: string;
			task_id?: string;
			attachments?: {
				file_upload_id: string;
				name: string;
				mime_type: string;
			}[];
		} = { content: trimmed };

		if (workbenchProjectId) {
			params.project_id = workbenchProjectId;
		}
		if (selectedTaskId) {
			params.task_id = selectedTaskId;
		}
		if (attachments?.length) {
			params.attachments = attachments
				.filter((attachment): attachment is Attachment & { fileUploadId: string } =>
					Boolean(attachment.fileUploadId?.trim()),
				)
				.map((attachment) => ({
					file_upload_id: attachment.fileUploadId.trim(),
					name: attachment.name,
					mime_type: attachment.mimeType || attachment.file?.type || "application/octet-stream",
				}));
		}

		try {
			const res = await workApi.newMessage(params);
			const data = res.data.data;
			if (data?.project_id && data?.task_id && data?.session_id) {
				this.#set({
					activeProjectId: data.project_id,
					activeWorkbenchProjectId: null,
					activeWorkbenchTaskId: null,
					activeTaskDetailProjectId: data.project_id,
					activeTaskDetailTaskId: data.task_id,
					activeTaskDetailSessionId: data.session_id,
					currentView: "taskDetail",
					conversationListOpen: false,
				});
			}
			return data ?? null;
		} catch (err) {
			console.error("sendWorkbenchMessage error:", err);
			return null;
		}
	};

	openTaskDetail = (projectId: string, taskId: string, sessionId: string | null = null) => {
		this.#set({
			activeTaskDetailProjectId: projectId,
			activeTaskDetailTaskId: taskId,
			activeTaskDetailSessionId: sessionId,
			currentView: "taskDetail",
		});
	};

	setTaskDetailRoute = (projectId: string, taskId: string, sessionId: string | null = null) => {
		this.#set({
			activeProjectId: projectId,
			activeTaskDetailProjectId: projectId,
			activeTaskDetailTaskId: taskId,
			activeTaskDetailSessionId: sessionId,
			currentView: "taskDetail",
			conversationListOpen: false,
		});
	};

	fetchProjects = async () => {
		try {
			const res = await projectApi.list({ list_all: true, limit: 100 });
			const items = res.data.data?.items ?? [];
			const apiProjects = items.map(mapBackendProject);
			this.#set((state) => {
				const localProjects = state.projects.filter(
					(p) => !apiProjects.some((ap) => ap.id === p.id),
				);
				return { projects: apiProjects.length ? [...apiProjects, ...localProjects] : [] };
			});
		} catch (err) {
			console.error("fetchProjects error:", err);
		}
	};

	createProject = async (params: {
		name: string;
		description?: string;
		metadata?: Record<string, unknown>;
	}) => {
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
				activeWorkbenchProjectId:
					state.activeWorkbenchProjectId === publicId ? null : state.activeWorkbenchProjectId,
				activeWorkbenchTaskId:
					state.activeWorkbenchProjectId === publicId ? null : state.activeWorkbenchTaskId,
			}));
			return true;
		} catch (err) {
			console.error("deleteProject error:", err);
			return false;
		}
	};

	fetchTasks = async (projectId: string) => {
		const project = this.#get().projects.find((p) => p.id === projectId);
		if (!project) return;

		try {
			const res = await projectApi.detail({ public_id: projectId });
			const detail = res.data.data;
			if (!detail) throw new Error("No data returned");
			const tasks = (detail.tasks ?? []).map(mapBackendTask);
			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === projectId
						? {
								...p,
								name: detail.name,
								description: detail.description ?? "",
								objective: detail.objective,
								updatedAt: new Date(detail.updated_at).getTime(),
								tasks,
							}
						: p,
				),
				projectSessionId: detail.session?.session_id ?? s.projectSessionId,
				projectSessionProjectId: detail.session?.session_id ? projectId : s.projectSessionProjectId,
			}));
		} catch (err) {
			console.error("fetchTasks error:", err);
		}
	};

	createTask = async (
		projectId: string,
		params: {
			title: string;
			description?: string;
			assignee_id?: number;
			task_type?: string;
			deadline?: string;
			metadata?: Record<string, unknown>;
		},
	) => {
		const state = this.#get();
		const project = state.projects.find((p) => p.id === projectId);
		if (!project) return null;

		try {
			const res = await taskApi.create({ project_id: projectId, ...params });
			const bt = res.data.data;
			if (!bt) throw new Error("No data returned");
			const item = mapBackendTask(bt);
			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === projectId ? { ...p, tasks: [item, ...p.tasks], updatedAt: Date.now() } : p,
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

	fetchProjectDetail = async (projectId: string) => {
		const project = this.#get().projects.find((p) => p.id === projectId);
		if (!project) return;

		this.#set({ projectDetailLoading: true, projectDetailError: null });
		try {
			const res = await projectApi.detail({ public_id: projectId });
			const detail = res.data.data;
			if (!detail) throw new Error("No data returned");

			const tasks = (detail.tasks ?? []).map(mapBackendTask);
			this.#set((s) => ({
				projects: s.projects.map((p) =>
					p.id === projectId
						? {
								...p,
								name: detail.name,
								description: detail.description ?? "",
								objective: detail.objective,
								updatedAt: new Date(detail.updated_at).getTime(),
								tasks,
								artifacts: [],
								files: [],
							}
						: p,
				),
				projectDetailLoading: false,
				projectSessionId: detail.session?.session_id ?? null,
				projectSessionProjectId: detail.session?.session_id ? projectId : null,
			}));
		} catch (err) {
			console.error("fetchProjectDetail error:", err);
			this.#set({ projectDetailLoading: false, projectDetailError: "获取项目详情失败" });
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

	resetAuthScopedData = () => {
		this.#set({
			currentView: "workbench",
			activeConversationId: null,
			activeProjectId: null,
			activeWorkbenchProjectId: null,
			activeWorkbenchTaskId: null,
			activeProjectTab: "chat",
			projects: [],
			conversations: [],
			conversationsLoaded: false,
			activeTaskDetailProjectId: null,
			activeTaskDetailTaskId: null,
			activeTaskDetailSessionId: null,
			projectDetailLoading: false,
			projectDetailError: null,
			activeProjectSessionId: null,
			projectSessionId: null,
			projectSessionProjectId: null,
		});
	};
}

export const layoutSlice: SliceCreator<LayoutStore> = (...params) => ({
	..._initialState,
	...flattenActions<LayoutAction>([createLayoutSlice(params[0] as SetState, params[1])]),
});
