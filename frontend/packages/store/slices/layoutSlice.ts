import type { ProjectMemberInput } from "../api/projectApi";
import { projectApi } from "../api/projectApi";
import { type CreateInitialMessageParams, sessionApi } from "../api/sessionApi";
import { taskApi } from "../api/taskApi";
import type {
	BackendMessageMetadata,
	BackendProject,
	BackendProjectMemberItem,
	BackendSession,
	BackendTask,
} from "../api/types";
import { handlePermissionDenied } from "../permission/errors";
import type { SliceCreator } from "../types";
import type { Attachment, ComposerToken, MessageMetadata } from "../types/chat";
import { flattenActions } from "../utils";
import { readStoredAuthUser } from "../utils/authStorage";
import { parseOptionalTimestamp } from "../utils/format";
import {
	clampLeftRailWidth,
	readStoredLeftRailPreferences,
	writeStoredLeftRailCollapsed,
	writeStoredLeftRailWidth,
} from "../utils/leftRailStorage";
import { isSystemDefaultAssistant } from "./digitalAssistantSlice";

export {
	LEFT_RAIL_MAX_WIDTH,
	LEFT_RAIL_MIN_WIDTH,
} from "../utils/leftRailStorage";

const storedLeftRailPreferences = readStoredLeftRailPreferences();

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
	updatedAt?: number;
	sessionId?: string;
	taskType?: string;
	deadline?: string;
	description?: string;
	assistantId?: number;
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
	updatedAt?: number;
	downloadUrl: string;
	storageUri?: string;
	sha256?: string;
};

export type ProjectSkill = {
	code: string;
	name: string;
	description?: string;
	category?: string;
	source?: string;
	trust?: string;
};

export type ProjectTab = "chat" | "tasks" | "files" | "activity";

export type ProjectMemberType = "assistant" | "user";

export type ProjectMember = {
	id: string;
	memberId: number;
	publicId?: string;
	type: ProjectMemberType;
	role: string;
	name: string;
	description?: string;
	avatarUrl?: string;
	joinedAt?: string;
	isDefault?: boolean;
};

function buildBackendMessageMetadata(
	metadata?: MessageMetadata,
): BackendMessageMetadata | undefined {
	if (!metadata) return undefined;

	const extra: Record<string, unknown> = {};
	if (metadata.composerTokens?.length) extra.composerTokens = metadata.composerTokens;
	if (metadata.displayContent?.trim()) extra.displayContent = metadata.displayContent;
	if (metadata.displayComposerTokens?.length) {
		extra.displayComposerTokens = metadata.displayComposerTokens;
	}
	if (metadata.invokedAssistant) extra.invokedAssistant = metadata.invokedAssistant;

	// 中文注释：metadata.extra 是当前前后端已有扩展口，避免为展示态召唤信息新增后端字段。
	return Object.keys(extra).length > 0 ? { extra } : undefined;
}

export type Project = {
	id: string;
	name: string;
	description: string;
	objective?: string;
	metadata?: Record<string, unknown>;
	skills: ProjectSkill[];
	members: ProjectMember[];
	taskCount: number;
	createdAt: number;
	updatedAt: number;
	messages: ProjectMessage[];
	tasks: ProjectTask[];
	files: ProjectArtifact[];
};

export type ProjectComposerPrefill = {
	id: string;
	projectId: string;
	value: string;
	tokens: ComposerToken[];
};

export type WorkbenchComposerPrefill = {
	id: string;
	value: string;
	tokens: ComposerToken[];
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
	| "projectsHub"
	| "taskDetail"
	| "digitalAssistant"
	| "aiTeammates"
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
	activeProjectTab: ProjectTab;
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
	projectComposerPrefill: ProjectComposerPrefill | null;
	workbenchComposerPrefill: WorkbenchComposerPrefill | null;
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
	const metadata = bp.metadata ?? undefined;
	const backendMembers = (bp as BackendProject & { members?: BackendProjectMemberItem[] }).members;
	return {
		id: bp.public_id,
		name: bp.name,
		description: bp.description ?? "",
		taskCount: bp.task_count ?? 0,
		createdAt: new Date(bp.created_at).getTime(),
		updatedAt: new Date(bp.updated_at).getTime(),
		metadata,
		skills: extractProjectSkills(metadata),
		members:
			backendMembers && backendMembers.length > 0
				? backendMembers.map(mapBackendProjectMember)
				: extractProjectMembers(metadata),
		messages: [],
		tasks: [],
		files: [],
	};
}

export function mergeProjectsFromListResult(
	apiProjects: Project[],
	localProjects: Project[],
): Project[] {
	const localProjectMap = new Map(localProjects.map((project) => [project.id, project]));
	const mergedApiProjects = apiProjects.map((project) => {
		const localProject = localProjectMap.get(project.id);
		if (!localProject) {
			return project;
		}

		return {
			...project,
			// 中文注释：列表接口只提供项目基础信息，这里保留本地已经加载过的详情字段，避免切页时把任务树清空。
			objective: project.objective ?? localProject.objective,
			members: project.members.length > 0 ? project.members : localProject.members,
			messages: project.messages.length > 0 ? project.messages : localProject.messages,
			tasks: project.tasks.length > 0 ? project.tasks : localProject.tasks,
			files: project.files.length > 0 ? project.files : localProject.files,
		};
	});

	// 中文注释：列表接口已按分页拉取完整项目集，因此这里只保留接口中仍存在的项目，避免已删除项目继续残留在本地状态里。
	return mergedApiProjects;
}

function mapBackendProjectMember(member: BackendProjectMemberItem): ProjectMember {
	const type = normalizeProjectMemberType(member.member_type);
	const publicId = member.public_id;
	return {
		id: publicId ? `${type}-${publicId}` : `${type}-${member.member_id}`,
		memberId: member.member_id,
		publicId,
		type,
		role: member.member_role || "member",
		name: member.name || (type === "assistant" ? "AI 队友" : "项目队友"),
		description: member.description,
		avatarUrl: member.avatar_url,
		joinedAt: member.joined_at,
		isDefault: member.is_default || (type === "assistant" && isSystemDefaultAssistant(publicId)),
	};
}

function normalizeProjectMemberType(value: string): ProjectMemberType {
	const normalized = value.toLowerCase();
	if (normalized === "assistant" || normalized === "ai" || normalized === "digital_assistant") {
		return "assistant";
	}
	return "user";
}

function extractProjectMembers(metadata?: Record<string, unknown>): ProjectMember[] {
	const extra = metadata?.extra;
	if (!extra || typeof extra !== "object" || Array.isArray(extra)) return [];

	const rawMembers = (extra as Record<string, unknown>).members;
	if (!Array.isArray(rawMembers)) return [];

	return rawMembers
		.map((item): ProjectMember | null => {
			if (!item || typeof item !== "object" || Array.isArray(item)) return null;
			const data = item as Record<string, unknown>;
			const memberId = Number(data.memberId ?? data.member_id);
			const rawType = typeof data.type === "string" ? data.type : String(data.member_type ?? "");
			const type = normalizeProjectMemberType(rawType);
			const name = typeof data.name === "string" ? data.name : "";
			const publicId =
				typeof data.publicId === "string"
					? data.publicId
					: typeof data.public_id === "string"
						? data.public_id
						: typeof data.id === "string"
							? data.id.replace(/^(assistant|human|user)-/, "")
							: undefined;
			if (!Number.isFinite(memberId) || !name) return null;

			return {
				id:
					typeof data.id === "string" && data.id
						? data.id
						: publicId
							? `${type}-${publicId}`
							: `${type}-${memberId}`,
				memberId,
				publicId,
				type,
				role:
					typeof data.role === "string"
						? data.role
						: typeof data.member_role === "string"
							? data.member_role
							: "member",
				name,
				description: typeof data.description === "string" ? data.description : undefined,
				avatarUrl:
					typeof data.avatarUrl === "string"
						? data.avatarUrl
						: typeof data.avatar_url === "string"
							? data.avatar_url
							: undefined,
				joinedAt:
					typeof data.joinedAt === "string"
						? data.joinedAt
						: typeof data.joined_at === "string"
							? data.joined_at
							: undefined,
				isDefault:
					type === "assistant" && isSystemDefaultAssistant(publicId)
						? true
						: typeof data.isDefault === "boolean"
							? data.isDefault
							: typeof data.is_default === "boolean"
								? data.is_default
								: undefined,
			};
		})
		.filter((item): item is ProjectMember => item !== null);
}

export function projectMembersToInputs(members: ProjectMember[]): ProjectMemberInput[] {
	return members
		.filter(
			(member) =>
				Boolean(member.publicId) &&
				!(
					member.type === "assistant" &&
					(member.isDefault || isSystemDefaultAssistant(member.publicId))
				),
		)
		.map((member) => ({
			type: member.type,
			// 中文注释：成员更新接口要求 AI 员工和真实成员都传 public_id，默认 AI 由后端保留不参与 diff。
			id: member.publicId as string,
			// 中文注释：仅真人成员携带项目角色，AI 队友无角色概念，交由后端忽略。
			...(member.type === "user" ? { role: member.role || "member" } : {}),
		}));
}
function extractProjectSkills(metadata?: Record<string, unknown>): ProjectSkill[] {
	const extra = metadata?.extra;
	if (!extra || typeof extra !== "object" || Array.isArray(extra)) return [];

	const rawSkills = (extra as Record<string, unknown>).skills;
	if (!Array.isArray(rawSkills)) return [];

	return rawSkills
		.map((item): ProjectSkill | null => {
			if (!item || typeof item !== "object" || Array.isArray(item)) return null;
			const data = item as Record<string, unknown>;
			const name = typeof data.name === "string" ? data.name : "";
			const code = typeof data.code === "string" ? data.code : name;
			if (!code || !name) return null;

			return {
				code,
				name,
				description: typeof data.description === "string" ? data.description : undefined,
				category: typeof data.category === "string" ? data.category : undefined,
				source: typeof data.source === "string" ? data.source : undefined,
				trust: typeof data.trust === "string" ? data.trust : undefined,
			};
		})
		.filter((item): item is ProjectSkill => item !== null);
}

function mapBackendTask(bt: BackendTask): ProjectTask {
	const taskWithSession = bt as BackendTask & { session?: BackendSession };
	const rawAssistantId = taskWithSession.session?.assistant_id;
	const assistantId = rawAssistantId !== undefined ? Number(rawAssistantId) : undefined;
	return {
		id: bt.public_id,
		title: bt.title,
		meta: bt.description ?? bt.task_type ?? "",
		status: (bt.status as ProjectTaskStatus) ?? "todo",
		// 中文注释：保留任务更新时间，供左侧最近项目列表展示相对时间。
		updatedAt: parseOptionalTimestamp(bt.updated_at),
		sessionId: taskWithSession.session?.session_id,
		taskType: bt.task_type,
		deadline: bt.deadline,
		description: bt.description,
		// 中文注释：后端 session.assistant_id 以字符串返回，前端任务模型统一保存数字 ID。
		assistantId:
			assistantId !== undefined && Number.isFinite(assistantId) ? assistantId : undefined,
	};
}

const _initialState: LayoutState = {
	leftRailCollapsed: storedLeftRailPreferences.collapsed,
	leftRailWidth: storedLeftRailPreferences.width,
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
				{ id: "workbench", label: "新建任务", icon: "IconTask" },
				{ id: "ai-teammates", label: "AI队友", icon: "IconAITeammate" },
				{ id: "projects-hub", label: "项目", icon: "IconProjectsHub" },
				{ id: "skills", label: "技能库", icon: "IconSkill" },
				{ id: "knowledge", label: "知识库", icon: "IconKnowledge" },
			],
		},
		{
			id: "projects",
			label: "项目",
			items: [],
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
	projectComposerPrefill: null,
	workbenchComposerPrefill: null,
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
	#fetchProjectsPromise: Promise<void> | null = null;
	#fetchProjectDetailPromises = new Map<string, Promise<void>>();
	#projectDetailLoadedIds = new Set<string>();
	#projectsFetchEpoch = 0;

	constructor(set: SetState, get: () => LayoutStore) {
		this.#set = set;
		this.#get = get;
	}

	#clearComposerDraft = () => {
		const store = this.#get() as LayoutStore & {
			clearComposerInput?: () => void;
		};
		// 中文注释：项目/任务聊天输入框与首页共用同一份草稿状态，离开当前上下文时必须同步清空，避免 token 退化成普通文本残留。
		store.clearComposerInput?.();
	};

	// 中文注释：工作台新建/续聊任务后，在跳转任务详情前写入等待态，避免详情页空屏或长时间无反馈。
	#bootstrapWorkbenchTaskSession = (
		sessionId: string,
		content: string,
		attachments?: Attachment[],
		metadata?: MessageMetadata,
	) => {
		const trimmed = content.trim();
		if (!sessionId || !trimmed) return;
		const store = this.#get() as LayoutStore & {
			bootstrapNewTaskSession?: (
				sessionId: string,
				content: string,
				options?: {
					attachments?: Attachment[];
					metadata?: MessageMetadata;
				},
			) => void;
			startGlobalEvents?: () => Promise<void>;
		};
		void store.startGlobalEvents?.();
		store.bootstrapNewTaskSession?.(sessionId, trimmed, {
			attachments,
			metadata,
		});
	};

	toggleLeftRail = () => {
		this.setLeftRailCollapsed(!this.#get().leftRailCollapsed);
	};

	setLeftRailCollapsed = (collapsed: boolean) => {
		writeStoredLeftRailCollapsed(collapsed);
		this.#set({ leftRailCollapsed: collapsed });
	};

	setLeftRailWidth = (width: number) => {
		const nextWidth = clampLeftRailWidth(width);
		writeStoredLeftRailWidth(nextWidth);
		this.#set({ leftRailWidth: nextWidth });
	};

	toggleConversationList = () => {
		this.#set((state) => ({
			conversationListOpen: !state.conversationListOpen,
		}));
	};

	switchView = (view: ViewMode) => {
		const state = this.#get();
		if (state.currentView !== view) {
			this.#clearComposerDraft();
		}
		this.#set({
			currentView: view,
			conversationListOpen: view === "chat",
			...(view === "workbench"
				? {
						activeWorkbenchProjectId: null,
						activeWorkbenchTaskId: null,
					}
				: {}),
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
		const state = this.#get();
		const keepsPendingPrefill = state.projectComposerPrefill?.projectId === projectId;
		if (
			!keepsPendingPrefill &&
			(state.currentView !== "project" || state.activeProjectId !== projectId)
		) {
			this.#clearComposerDraft();
		}
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

	setProjectRoute = (projectId: string, tab: ProjectTab = "chat") => {
		const state = this.#get();
		const keepsPendingPrefill = state.projectComposerPrefill?.projectId === projectId;
		if (
			!keepsPendingPrefill &&
			(state.currentView !== "project" || state.activeProjectId !== projectId)
		) {
			this.#clearComposerDraft();
		}
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
		this.#set({
			activeWorkbenchProjectId: projectId,
			activeWorkbenchTaskId: null,
		});
		if (projectId) {
			this.fetchTasks(projectId);
		}
	};

	selectWorkbenchTask = (taskId: string | null) => {
		this.#set({ activeWorkbenchTaskId: taskId });
	};

	setActiveProjectTab = (tab: ProjectTab) => {
		this.#set({ activeProjectTab: tab });
	};

	setProjectComposerPrefill = (prefill: Omit<ProjectComposerPrefill, "id">) => {
		this.#set({
			projectComposerPrefill: {
				...prefill,
				id: `prefill_${Date.now()}_${Math.random().toString(36).slice(2)}`,
			},
		});
	};

	consumeProjectComposerPrefill = (prefillId: string) => {
		this.#set((state) => ({
			projectComposerPrefill:
				state.projectComposerPrefill?.id === prefillId ? null : state.projectComposerPrefill,
		}));
	};

	setWorkbenchComposerPrefill = (prefill: Omit<WorkbenchComposerPrefill, "id">) => {
		this.#set({
			workbenchComposerPrefill: {
				...prefill,
				id: `workbench_prefill_${Date.now()}_${Math.random().toString(36).slice(2)}`,
			},
		});
	};

	consumeWorkbenchComposerPrefill = (prefillId: string) => {
		this.#set((state) => ({
			workbenchComposerPrefill:
				state.workbenchComposerPrefill?.id === prefillId ? null : state.workbenchComposerPrefill,
		}));
	};

	sendWorkbenchMessage = async (
		content: string,
		projectId?: string | null,
		executionMode?: "default" | "plan",
		attachments?: Attachment[],
		_metadata?: MessageMetadata,
		assistantIds?: string[],
	) => {
		const trimmed = content.trim();
		// 中文注释：允许空内容 + assistant_ids 召唤队友落地空对话，仅创建任务会话不发送首条消息。
		if (!trimmed && !assistantIds?.length) return;
		const mode = executionMode ?? "default";

		const state = this.#get();
		const workbenchProjectId = projectId ?? state.activeWorkbenchProjectId;
		const selectedTaskId = workbenchProjectId ? state.activeWorkbenchTaskId : null;

		if (workbenchProjectId && selectedTaskId) {
			let project = state.projects.find((p) => p.id === workbenchProjectId);
			let selectedTask = project?.tasks.find((task) => task.id === selectedTaskId);

			if (!selectedTask?.sessionId) {
				try {
					const detailRes = await projectApi.detail({
						public_id: workbenchProjectId,
					});
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
					const globalEventsStore = this.#get() as LayoutStore & {
						startGlobalEvents?: () => Promise<void>;
					};
					void globalEventsStore.startGlobalEvents?.();
					await sessionApi.addMessage({
						session_id: selectedTask.sessionId,
						role: "user",
						content: trimmed,
						execution_mode: mode,
						message_type: "text",
						metadata: buildBackendMessageMetadata(_metadata),
						attachments: attachments
							?.filter((attachment): attachment is Attachment & { fileUploadId: string } =>
								Boolean(attachment.fileUploadId?.trim()),
							)
							.map((attachment) => ({
								file_upload_id: attachment.fileUploadId.trim(),
								name: attachment.name,
								mime_type:
									attachment.mimeType || attachment.file?.type || "application/octet-stream",
								size: attachment.size,
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
						executionMode: mode,
					} as Partial<LayoutState>);
					await this.saveWorkbenchRecentContext(data.project_id, data.task_id);
					this.#bootstrapWorkbenchTaskSession(data.session_id, trimmed, attachments, _metadata);
					return data;
				} catch (err) {
					console.error("sendWorkbenchMessage addMessage error:", err);
					return null;
				}
			}
		}

		const params: CreateInitialMessageParams = { content: trimmed, execution_mode: mode };
		if (assistantIds?.length) {
			// 中文注释：后端 NewMessageRequest 只接收 publicId 字符串数组 assistant_ids。
			params.assistant_ids = assistantIds;
		}

		if (workbenchProjectId) {
			params.project_id = workbenchProjectId;
		}
		if (selectedTaskId) {
			params.task_id = selectedTaskId;
		}
		const backendMetadata = buildBackendMessageMetadata(_metadata);
		if (backendMetadata) {
			// 中文注释：首页新建任务需要透传输入框展示元信息，避免 @队友 回显退化成默认 Lework。
			params.metadata = backendMetadata;
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
					size: attachment.size,
				}));
		}

		try {
			const globalEventsStore = this.#get() as LayoutStore & {
				startGlobalEvents?: () => Promise<void>;
			};
			void globalEventsStore.startGlobalEvents?.();
			const res = await sessionApi.createInitialMessage(params);
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
					executionMode: mode,
				} as Partial<LayoutState>);
				await this.saveWorkbenchRecentContext(data.project_id, data.task_id);
				// 新建项目/任务后立即拉详情，确保 store 有数据供 SSE 标题 patch 与详情页展示。
				await this.fetchProjectDetail(data.project_id);
				this.#bootstrapWorkbenchTaskSession(data.session_id, trimmed, attachments, _metadata);
			}
			return data ?? null;
		} catch (err) {
			console.error("sendWorkbenchMessage error:", err);
			return null;
		}
	};

	openTaskDetail = (projectId: string, taskId: string, sessionId: string | null = null) => {
		const state = this.#get();
		if (
			state.currentView !== "taskDetail" ||
			state.activeTaskDetailProjectId !== projectId ||
			state.activeTaskDetailTaskId !== taskId ||
			state.activeTaskDetailSessionId !== sessionId
		) {
			this.#clearComposerDraft();
		}
		this.#set({
			activeTaskDetailProjectId: projectId,
			activeTaskDetailTaskId: taskId,
			activeTaskDetailSessionId: sessionId,
			currentView: "taskDetail",
		});
	};

	setTaskDetailRoute = (projectId: string, taskId: string, sessionId: string | null = null) => {
		const state = this.#get();
		if (
			state.currentView !== "taskDetail" ||
			state.activeTaskDetailProjectId !== projectId ||
			state.activeTaskDetailTaskId !== taskId ||
			state.activeTaskDetailSessionId !== sessionId
		) {
			this.#clearComposerDraft();
		}
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
		if (!readStoredAuthUser()?.jwtToken) return;
		if (this.#fetchProjectsPromise) return this.#fetchProjectsPromise;

		const fetchEpoch = this.#projectsFetchEpoch;
		this.#fetchProjectsPromise = (async () => {
			try {
				const pageSize = 100;
				let offset = 0;
				let total = Number.POSITIVE_INFINITY;
				const items: BackendProject[] = [];

				// 中文注释：多个页面壳会同时请求项目列表，复用同一个分页拉取流程，避免刷新时重复打 ListProjects。
				while (offset < total) {
					const res = await projectApi.list({ offset, limit: pageSize });
					const data = res.data.data;
					const pageItems = data?.items ?? [];
					total = data?.total ?? 0;
					items.push(...pageItems);
					if (pageItems.length === 0) break;
					offset += pageItems.length;
				}

				if (fetchEpoch !== this.#projectsFetchEpoch) return;

				const apiProjects = items.map(mapBackendProject);
				this.#set((state) => ({
					projects: apiProjects.length
						? mergeProjectsFromListResult(apiProjects, state.projects)
						: [],
				}));
			} catch (err) {
				console.error("fetchProjects error:", err);
			} finally {
				if (fetchEpoch === this.#projectsFetchEpoch) {
					this.#fetchProjectsPromise = null;
				}
			}
		})();

		return this.#fetchProjectsPromise;
	};

	createProject = async (params: {
		name: string;
		description?: string;
		members?: ProjectMemberInput[];
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
		members?: ProjectMemberInput[];
		metadata?: Record<string, unknown>;
	}) => {
		try {
			const res = await projectApi.update(params);
			const bp = res.data.data;
			if (!bp) throw new Error("No data returned");
			const item = mapBackendProject(bp);
			this.#set((state) => ({
				projects: state.projects.map((p) =>
					p.id === item.id
						? {
								...p,
								...item,
								tasks: p.tasks,
								members: item.members.length > 0 ? item.members : p.members,
								messages: p.messages,
								files: p.files,
							}
						: p,
				),
			}));
			const store = this.#get() as LayoutStore & {
				invalidate?: (resource?: { type: "project"; publicId: string }) => void;
			};
			store.invalidate?.({ type: "project", publicId: params.public_id });
			return item;
		} catch (err) {
			if (handlePermissionDenied(err)) return null;
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
			if (handlePermissionDenied(err)) return false;
			console.error("deleteProject error:", err);
			return false;
		}
	};

	leaveProject = async (publicId: string) => {
		try {
			await projectApi.leave({ public_id: publicId });
			this.#set((state) => ({
				projects: state.projects.filter((p) => p.id !== publicId),
				activeProjectId: state.activeProjectId === publicId ? null : state.activeProjectId,
				activeWorkbenchProjectId:
					state.activeWorkbenchProjectId === publicId ? null : state.activeWorkbenchProjectId,
				activeWorkbenchTaskId:
					state.activeWorkbenchProjectId === publicId ? null : state.activeWorkbenchTaskId,
				activeTaskDetailProjectId:
					state.activeTaskDetailProjectId === publicId ? null : state.activeTaskDetailProjectId,
				activeTaskDetailTaskId:
					state.activeTaskDetailProjectId === publicId ? null : state.activeTaskDetailTaskId,
				activeTaskDetailSessionId:
					state.activeTaskDetailProjectId === publicId ? null : state.activeTaskDetailSessionId,
			}));
			const store = this.#get() as LayoutStore & {
				invalidate?: (resource?: { type: "project"; publicId: string }) => void;
			};
			store.invalidate?.({ type: "project", publicId });
			return true;
		} catch (err) {
			if (handlePermissionDenied(err)) return false;
			console.error("leaveProject error:", err);
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
			if (handlePermissionDenied(err)) return null;
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
			if (handlePermissionDenied(err)) return null;
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
			if (handlePermissionDenied(err)) return;
			console.error("deleteTask error:", err);
		}
	};

	applyWorkTitleUpdated = (payload: {
		project_id: string;
		project_name: string;
		task_id?: string;
		task_title?: string;
		session_id?: string;
		session_title?: string;
	}) => {
		this.#set((state) => {
			const existing = state.projects.find((project) => project.id === payload.project_id);
			const updatedConversations =
				payload.session_id && payload.session_title
					? state.conversations.map((conversation) =>
							conversation.id === payload.session_id
								? {
										...conversation,
										title: payload.session_title ?? conversation.title,
										updatedAt: Date.now(),
									}
								: conversation,
						)
					: state.conversations;
			if (!existing) {
				const now = Date.now();
				const task =
					payload.task_id != null
						? [
								{
									id: payload.task_id,
									title: payload.task_title ?? payload.project_name,
									meta: "",
									status: "todo" as const,
									updatedAt: now,
									sessionId: payload.session_id,
								},
							]
						: [];
				return {
					projects: [
						{
							id: payload.project_id,
							name: payload.project_name,
							description: "",
							skills: [],
							members: [],
							taskCount: 0,
							createdAt: now,
							updatedAt: now,
							messages: [],
							tasks: task,
							files: [],
						},
						...state.projects,
					],
					conversations: updatedConversations,
				};
			}

			return {
				projects: state.projects.map((project) => {
					if (project.id !== payload.project_id) return project;
					return {
						...project,
						name: payload.project_name,
						updatedAt: Date.now(),
						tasks: project.tasks.map((task) =>
							payload.task_id && task.id === payload.task_id
								? {
										...task,
										title: payload.task_title ?? task.title,
										sessionId: payload.session_id ?? task.sessionId,
									}
								: task,
						),
					};
				}),
				conversations: updatedConversations,
			};
		});
	};

	fetchProjectDetail = async (projectId: string) => {
		const inflight = this.#fetchProjectDetailPromises.get(projectId);
		if (inflight) return inflight;

		const promise = this.#loadProjectDetail(projectId);
		this.#fetchProjectDetailPromises.set(projectId, promise);
		try {
			await promise;
		} finally {
			this.#fetchProjectDetailPromises.delete(projectId);
		}
	};

	#loadProjectDetail = async (projectId: string) => {
		const isInitialLoad = !this.#projectDetailLoadedIds.has(projectId);
		if (isInitialLoad) {
			this.#set({ projectDetailLoading: true, projectDetailError: null });
		}

		try {
			const res = await projectApi.detail({ public_id: projectId });
			const detail = res.data.data;
			if (!detail) throw new Error("No data returned");

			const tasks = (detail.tasks ?? []).map(mapBackendTask);
			const mapped = mapBackendProject(detail);
			this.#projectDetailLoadedIds.add(projectId);
			this.#set((s) => {
				const exists = s.projects.some((project) => project.id === projectId);
				return {
					projects: exists
						? s.projects.map((p) =>
								p.id === projectId
									? {
											...mapped,
											objective: detail.objective,
											tasks,
											files: [],
											// 中文注释：task_count 仅由 ListProjects 提供，详情接口不覆盖该字段。
											taskCount: p.taskCount,
											updatedAt: new Date(detail.updated_at).getTime(),
										}
									: p,
							)
						: [
								{
									...mapped,
									objective: detail.objective,
									tasks,
									files: [],
								},
								...s.projects,
							],
					projectDetailLoading: false,
					projectSessionId: detail.session?.session_id ?? null,
					projectSessionProjectId: detail.session?.session_id ? projectId : null,
				};
			});
		} catch (err) {
			console.error("fetchProjectDetail error:", err);
			if (isInitialLoad) {
				this.#set({ projectDetailLoading: false, projectDetailError: "获取项目详情失败" });
			}
		}
	};

	fetchRecentWorkbenchContext = async () => {
		// 中文注释：新建任务页默认停留在「新建项目/任务」，不恢复最近使用的项目/任务选择。
	};

	saveWorkbenchRecentContext = async (projectId: string, taskId?: string | null) => {
		if (!projectId) return;
		try {
			await projectApi.saveWorkbenchRecentContext({
				project_id: projectId,
				task_id: taskId ?? null,
			});
		} catch (err) {
			console.error("saveWorkbenchRecentContext error:", err);
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
		this.#projectsFetchEpoch += 1;
		this.#fetchProjectsPromise = null;
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
			projectComposerPrefill: null,
			workbenchComposerPrefill: null,
		});
	};
}

export const layoutSlice: SliceCreator<LayoutStore> = (...params) => ({
	..._initialState,
	...flattenActions<LayoutAction>([createLayoutSlice(params[0] as SetState, params[1])]),
});
