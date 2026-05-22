import { sessionApi } from "../api/sessionApi";
import type { BackendSession } from "../api/types";
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

const now = Date.now();

const mockProjects: Project[] = [
	{
		id: "backend-v2",
		name: "backend-v2",
		description: "后端 API 与数据库性能优化",
		updatedAt: now - 2 * 60 * 1000,
		messages: [
			{
				id: "backend-v2-msg-1",
				role: "assistant",
				content:
					"我已经分析了 backend-v2 当前数据库 schema，建议为高频访问的用户会话加入 Redis 缓存层，以降低 PostgreSQL 负载。",
				timestamp: now - 12 * 60 * 1000,
			},
			{
				id: "backend-v2-msg-2",
				role: "user",
				content: "听起来不错。可以说明 Docker 配置需要如何调整吗？",
				timestamp: now - 10 * 60 * 1000,
			},
		],
		tasks: [
			{ id: "task-1", title: "更新 session TTL", meta: "2 小时内到期 · 高优先级", status: "todo" },
			{ id: "task-2", title: "Docker 配置优化", meta: "等待 AI 草稿", status: "in_progress" },
			{ id: "task-3", title: "实现 Redis cache", meta: "已分配给 AI", status: "todo" },
			{ id: "task-4", title: "数据库查询压测", meta: "等待评审", status: "done" },
		],
		artifacts: [
			{
				id: "artifact-1",
				name: "schema_v2.json",
				type: "document",
				size: "4.2 KB",
				updatedAt: "12 分钟前",
			},
			{
				id: "artifact-2",
				name: "load_metrics.csv",
				type: "spreadsheet",
				size: "128 KB",
				updatedAt: "1 小时前",
			},
			{
				id: "artifact-3",
				name: "architecture_v1.png",
				type: "image",
				size: "1.8 MB",
				updatedAt: "3 小时前",
			},
		],
		files: [
			{
				id: "file-1",
				name: "api-contract.md",
				type: "document",
				size: "9.6 KB",
				updatedAt: "昨天",
			},
			{
				id: "file-2",
				name: "docker-compose.yml",
				type: "document",
				size: "3.1 KB",
				updatedAt: "2 天前",
			},
		],
		memories: [
			{
				id: "memory-1",
				title: "性能目标",
				content: "查询链路 P95 需要低于 180ms，缓存命中率目标 70% 以上。",
			},
		],
	},
	{
		id: "frontend-core",
		name: "frontend-core",
		description: "前端 UI 组件化重构",
		updatedAt: now - 45 * 60 * 1000,
		messages: [
			{
				id: "frontend-msg-1",
				role: "assistant",
				content: "我会把桌面端与 Web 端共用的组件收敛到 app-ui，并保持 store 与 UI 的边界清晰。",
				timestamp: now - 45 * 60 * 1000,
			},
		],
		tasks: [
			{ id: "task-5", title: "前端 UI 组件化重构", meta: "进行中", status: "in_progress" },
			{ id: "task-6", title: "更新 app-ui 文档", meta: "已完成", status: "done" },
		],
		artifacts: [
			{
				id: "artifact-4",
				name: "app-ui-plan.md",
				type: "document",
				size: "7.4 KB",
				updatedAt: "45 分钟前",
			},
		],
		files: [
			{
				id: "file-3",
				name: "component-map.md",
				type: "document",
				size: "6.3 KB",
				updatedAt: "今天",
			},
		],
		memories: [
			{
				id: "memory-2",
				title: "组件边界",
				content: "app-ui 负责跨端视图，store 负责状态与后端契约。",
			},
		],
	},
	{
		id: "incidents",
		name: "incidents",
		description: "热点故障复盘与报告",
		updatedAt: now - 3 * 60 * 60 * 1000,
		messages: [
			{
				id: "incidents-msg-1",
				role: "assistant",
				content: "我已经整理出最近 24 小时的故障时间线，并标记了需要补充根因证据的节点。",
				timestamp: now - 3 * 60 * 60 * 1000,
			},
		],
		tasks: [{ id: "task-7", title: "热点故障复盘报告", meta: "进行中", status: "in_progress" }],
		artifacts: [
			{
				id: "artifact-5",
				name: "incident-review.md",
				type: "document",
				size: "15 KB",
				updatedAt: "3 小时前",
			},
		],
		files: [],
		memories: [
			{
				id: "memory-3",
				title: "复盘格式",
				content: "复盘报告需要包含影响范围、检测时间、恢复时间和预防动作。",
			},
		],
	},
	{
		id: "infra",
		name: "infra",
		description: "基础设施安全审计",
		updatedAt: now - 8 * 60 * 60 * 1000,
		messages: [],
		tasks: [{ id: "task-8", title: "基础设施安全审计", meta: "待处理", status: "todo" }],
		artifacts: [],
		files: [],
		memories: [],
	},
];

const _initialState: LayoutState = {
	leftRailCollapsed: false,
	rightRailCollapsed: false,
	conversationListOpen: true,
	currentView: "workbench",
	activeConversationId: null,
	activeWorkspaceId: null,
	activeProjectId: null,
	activeProjectTab: "chat",
	workspaces: [
		{ id: "remote-1", name: "远程工作区", mode: "remote", collapsed: false },
		{ id: "local-1", name: "本地工作区", mode: "local", collapsed: false },
	],
	projects: mockProjects,
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
			items: [{ id: "ai-1", label: "AI 1", icon: "IconAITeammate" }],
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
		this.#set({ activeProjectId: projectId });
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
		const nextTask: ProjectTask = {
			id: `${targetProjectId}-task-${timestamp}`,
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
