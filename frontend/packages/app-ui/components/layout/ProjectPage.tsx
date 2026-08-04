"use client";

import {
	Action,
	buildProjectCapabilityItems,
	buildTaskCapabilityItems,
	fetchFilePreviewByStorageUri,
	isSystemDefaultAssistant,
	type PluginComposerOption,
	type PluginListItem,
	type Project,
	type ProjectMember,
	type ProjectSkill,
	type ProjectTask,
	pluginApi,
	projectFileApi,
	projectMemberApi,
	projectMembersToInputs,
	useAppStore,
	useCan,
	useChatStore,
	useDAStore,
	useEnsureCapabilities,
	useLayoutStore,
	useProjectCapabilities,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
} from "@leros/ui/components/ui/command";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	ChevronDown,
	ChevronRight,
	ChevronsLeft,
	ChevronsRight,
	LoaderCircle,
	Pencil,
	Plus,
	Search,
	Sparkles,
	Trash2,
	X,
} from "lucide-react";
import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { PROJECT_NEW_TASK_HERO_OCTOPUS_SRC } from "../../assets";
import { useAuth } from "../auth";
import { MCPConnectorIcon } from "../common/MCPConnectorIcon";
import { renderHighlightedText } from "../common/searchText";
import { ChatInput } from "../input/ChatInput";
import {
	bindSkillToProject,
	ProjectSkillBindingError,
	useSkillPickerOptions,
} from "../input/useSkillPickerOptions";
import { CanGate } from "../permission/CanGate";
import {
	isSameProjectMember,
	ProjectMemberChip,
	ProjectMemberPickerDialog,
	projectMemberChipClassName,
	projectMemberListClassName,
	sortProjectMembers,
} from "../project-members/ProjectMemberPickerDialog";
import { canQuickRemoveProjectMember } from "../project-members/project-member-removal";
import { openProjectFilePreview } from "./file-preview-store";
import { PROJECT_FILE_VERSION_CHANGED_EVENT } from "./file-preview-utils";
import type { AppNavigation } from "./LeftRail";
import { ProjectActivityPanel } from "./ProjectActivityPanel";
import { ProjectFileTree } from "./ProjectFileTree";
import { getProjectChatLayoutClasses, type ProjectChatLayoutMode } from "./project-chat-layout";
import {
	buildProjectFileListParams,
	isProjectFileFlatDisplay,
	PROJECT_FILE_TYPE_FILTER_OPTIONS,
	type ProjectFileSourceFilter,
	type ProjectFileTypeFilter,
} from "./project-file-filters";
import { SIDEBAR_COMPACT_LIST_CLASS, TaskCardIcon } from "./project-file-type-icon";
import {
	collectSelectableFiles,
	filterProjectFileSearchResults,
	getProjectFileSearchSourceNodes,
	PROJECT_FILE_TABLE_ACTIONS_HEADER_CLASS,
	PROJECT_FILE_TABLE_GRID_CLASS,
	PROJECT_FILE_TABLE_LEADING_CELL_CLASS,
	PROJECT_FILE_TABLE_MIN_WIDTH_CLASS,
	type ProjectFileNode,
	parseProjectFileList,
} from "./project-files";
import { downloadProjectFolderAsZip, triggerBlobDownload } from "./project-folder-download";
import { TaskDeleteDialog } from "./TaskDeleteDialog";

const projectTabs = [
	{ id: "chat" as const, label: "新建任务" },
	{ id: "tasks" as const, label: "任务列表" },
	{ id: "files" as const, label: "项目文件" },
	{ id: "activity" as const, label: "动态" },
];

const PROJECT_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY = "leros-project-config-right-sidebar-width";
const PROJECT_RIGHT_SIDEBAR_COLLAPSED_STORAGE_KEY = "leros-project-right-sidebar-collapsed";
const PROJECT_RIGHT_SIDEBAR_DEFAULT_WIDTH = 352;
const PROJECT_RIGHT_SIDEBAR_MIN_WIDTH = 300;
const PROJECT_RIGHT_SIDEBAR_MAX_WIDTH = 440;
const PROJECT_RIGHT_SIDEBAR_WIDE_BREAKPOINT = 360;

type ProjectTab = (typeof projectTabs)[number]["id"];

export function ProjectPage({
	projectId,
	tab,
	onTabChange,
	navigation,
}: {
	projectId?: string;
	tab?: ProjectTab;
	onTabChange?: (tab: ProjectTab) => void;
	navigation?: AppNavigation;
}) {
	const {
		projects,
		activeProjectId,
		currentView,
		activeProjectTab,
		projectDetailLoading,
		projectDetailError,
		fetchProjects,
		setProjectRoute,
		setActiveProjectTab,
		fetchProjectDetail,
		openTaskDetail,
		updateProject,
		updateProjectMembers: updateProjectMembersStore,
	} = useLayoutStore((s) => s);

	const {
		activeSessionId,
		isGenerating,
		pendingBootstrapSessionId,
		setActiveSession,
		clearLocalMessages,
		hasSessionMessages,
		allMessagesBelongToSession,
		loadConversationMessages,
		resetLocalMessages,
	} = useChatStore((s) => s);

	const [activityRefreshKey, setActivityRefreshKey] = useState(0);
	const [projectDetailInitialized, setProjectDetailInitialized] = useState(false);
	const [rightSidebarWidth, setRightSidebarWidth] = useState(PROJECT_RIGHT_SIDEBAR_DEFAULT_WIDTH);
	const [rightSidebarCollapsed, setRightSidebarCollapsed] = useState(false);
	const hasLoadedRightSidebarPreferenceRef = useRef(false);

	const resolvedProjectId = projectId ?? activeProjectId;
	const resolvedTab = tab ?? activeProjectTab;
	const project =
		projects.find((item) => item.id === resolvedProjectId) ??
		(resolvedProjectId ? undefined : projects[0]);

	// 中文注释：项目「新建任务」tab 始终展示空状态，不复用任务详情 / 工作台 / 项目级 session。
	const resolvedSessionId = useMemo(() => {
		if (resolvedTab !== "chat" || currentView !== "project") return null;
		return pendingBootstrapSessionId;
	}, [resolvedTab, currentView, pendingBootstrapSessionId]);

	const handleOpenTask = (task: ProjectTask) => {
		if (!resolvedProjectId) return;
		if (!task.sessionId) {
			toast.warning("当前任务缺少会话，无法打开详情");
			return;
		}
		if (navigation) {
			navigation.goToTaskDetail(resolvedProjectId, task.id, task.sessionId);
			return;
		}
		openTaskDetail(resolvedProjectId, task.id, task.sessionId);
	};

	const handleBackToProjects = () => {
		navigation?.goToRoute("projectsHub");
	};

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	useEffect(() => {
		if (projectId) {
			setProjectRoute(projectId, tab ?? "chat");
		}
	}, [projectId, tab, setProjectRoute]);

	useEffect(() => {
		if (resolvedProjectId) {
			fetchProjectDetail(resolvedProjectId);
		}
	}, [resolvedProjectId, fetchProjectDetail]);

	useEffect(() => {
		setProjectDetailInitialized(false);
	}, [resolvedProjectId]);

	useEffect(() => {
		if (!projectDetailLoading && project) {
			setProjectDetailInitialized(true);
		}
	}, [projectDetailLoading, project]);

	useEffect(() => {
		if (typeof window === "undefined" || hasLoadedRightSidebarPreferenceRef.current) return;
		hasLoadedRightSidebarPreferenceRef.current = true;

		const savedWidth = window.localStorage.getItem(PROJECT_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY);
		const savedCollapsed = window.localStorage.getItem(PROJECT_RIGHT_SIDEBAR_COLLAPSED_STORAGE_KEY);

		if (savedWidth) {
			const parsedWidth = Number(savedWidth);
			if (Number.isFinite(parsedWidth)) {
				// 右侧栏宽度读取后立即限制范围，避免旧值把布局撑坏。
				setRightSidebarWidth(clampProjectRightSidebarWidth(parsedWidth));
			}
		}

		if (savedCollapsed) {
			setRightSidebarCollapsed(savedCollapsed === "true");
		}
	}, []);

	useEffect(() => {
		if (typeof window === "undefined" || !hasLoadedRightSidebarPreferenceRef.current) return;
		window.localStorage.setItem(PROJECT_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY, String(rightSidebarWidth));
	}, [rightSidebarWidth]);

	useEffect(() => {
		if (typeof window === "undefined" || !hasLoadedRightSidebarPreferenceRef.current) return;
		window.localStorage.setItem(
			PROJECT_RIGHT_SIDEBAR_COLLAPSED_STORAGE_KEY,
			String(rightSidebarCollapsed),
		);
	}, [rightSidebarCollapsed]);

	useEffect(() => {
		if (projectDetailLoading) return;
		if (!resolvedSessionId) {
			// 中文注释：新建任务 bootstrap 跳转任务详情时，保留等待态消息和 bootstrap 标记，避免详情页重复创建 assistant。
			if (pendingBootstrapSessionId) return;
			resetLocalMessages();
			return;
		}
		const nextSessionId = resolvedSessionId;
		setActiveSession(nextSessionId);
		const bootstrapPending = pendingBootstrapSessionId === nextSessionId;
		const sessionHasMessages = hasSessionMessages(nextSessionId);
		// 项目消息刚创建 session 并准备开流时，跳过这次自动拉历史，避免旧数据覆盖 optimistic 消息。
		if (bootstrapPending && sessionHasMessages) return;
		// 中文注释：bootstrap 期间消息被误清时等待 GlobalEvents 回填，避免与 SSE resume 重复开流。
		if (bootstrapPending && !sessionHasMessages) return;
		// 中文注释：发送中禁止再 load，避免冲掉乐观 waiting；再进页依赖离开时 clear 掉 isGenerating。
		if (
			isGenerating &&
			activeSessionId === nextSessionId &&
			sessionHasMessages &&
			allMessagesBelongToSession(nextSessionId)
		) {
			return;
		}
		if (!sessionHasMessages) {
			clearLocalMessages();
		}
		loadConversationMessages(nextSessionId, {
			resumeStream: !(bootstrapPending && sessionHasMessages),
		});
	}, [
		resolvedSessionId,
		projectDetailLoading,
		isGenerating,
		pendingBootstrapSessionId,
		activeSessionId,
		setActiveSession,
		hasSessionMessages,
		allMessagesBelongToSession,
		clearLocalMessages,
		loadConversationMessages,
		resetLocalMessages,
	]);

	// 离开项目页时清理消息并关闭 SSE；bootstrap 跳转期间保留等待态，避免 remount 后空屏。
	useEffect(() => {
		return () => {
			if (useAppStore.getState().pendingBootstrapSessionId) return;
			clearLocalMessages();
		};
	}, [clearLocalMessages]);

	const handleRightSidebarResizeStart = (event: React.PointerEvent<HTMLHRElement>) => {
		const startX = event.clientX;
		const startWidth = rightSidebarWidth;
		const pointerId = event.pointerId;
		const target = event.currentTarget;

		target.setPointerCapture(pointerId);

		const handlePointerMove = (moveEvent: PointerEvent) => {
			setRightSidebarWidth(
				clampProjectRightSidebarWidth(startWidth - (moveEvent.clientX - startX)),
			);
		};

		const handlePointerUp = () => {
			if (target.hasPointerCapture(pointerId)) {
				target.releasePointerCapture(pointerId);
			}
			target.removeEventListener("pointermove", handlePointerMove);
			target.removeEventListener("pointerup", handlePointerUp);
			target.removeEventListener("pointercancel", handlePointerUp);
		};

		target.addEventListener("pointermove", handlePointerMove);
		target.addEventListener("pointerup", handlePointerUp);
		target.addEventListener("pointercancel", handlePointerUp);
	};

	// 中文注释：项目四个 tab 均展示右侧项目配置栏，便于随时维护成员和技能。
	const showProjectSidebar = true;
	const projectChatLayoutMode: ProjectChatLayoutMode =
		showProjectSidebar && !rightSidebarCollapsed ? "sidebar-expanded" : "sidebar-collapsed";
	const isWideRightSidebar = rightSidebarWidth >= PROJECT_RIGHT_SIDEBAR_WIDE_BREAKPOINT;
	const rightSidebarWidthStyle = !rightSidebarCollapsed
		? { width: `${rightSidebarWidth}px` }
		: undefined;

	if (!project) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-app-bg)] text-[var(--leros-text-muted)]">
				暂无项目
			</div>
		);
	}

	if (resolvedProjectId && !projectDetailInitialized) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-surface)]">
				<div className="flex flex-col items-center gap-3">
					<LoaderCircle className="size-8 animate-spin text-[var(--leros-text-muted)]" />
					<p className="text-sm text-[var(--leros-text-muted)]">加载项目详情中...</p>
				</div>
			</div>
		);
	}

	if (projectDetailError) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-surface)]">
				<div className="flex flex-col items-center gap-3">
					<p className="text-sm text-[var(--leros-text-muted)]">{projectDetailError}</p>
				</div>
			</div>
		);
	}

	return (
		<div
			data-slot="project-page"
			className="flex h-full min-w-0 flex-1 flex-col bg-[var(--leros-surface)]"
		>
			<header className="flex h-12 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				<div className="flex items-center gap-3 text-[var(--leros-text-muted)]">
					{/* 中文注释：项目详情页顶部保留面包屑，方便从具体项目快速回到项目列表页。 */}
					<button
						type="button"
						onClick={handleBackToProjects}
						className="text-sm font-normal text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
					>
						项目
					</button>
					<ChevronRight className="size-4 text-[var(--leros-text-subtle)]" />
					<h1 className="max-w-[360px] truncate text-sm font-semibold text-[var(--leros-text-strong)]">
						{project.name}
					</h1>
				</div>
			</header>

			<nav className="flex h-[48px] shrink-0 items-end gap-8 border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				{projectTabs.map((currentTab) => (
					<button
						key={currentTab.id}
						type="button"
						onClick={() => {
							if (onTabChange) {
								onTabChange(currentTab.id);
								return;
							}
							setActiveProjectTab(currentTab.id);
						}}
						className={cn(
							"relative h-full px-1 pb-2 text-sm font-semibold transition-colors",
							resolvedTab === currentTab.id
								? "text-[var(--leros-primary)]"
								: "text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]",
						)}
					>
						{currentTab.label}
						{resolvedTab === currentTab.id && (
							<span className="absolute bottom-0 left-0 h-0.5 w-full rounded-full bg-[var(--leros-primary)]" />
						)}
					</button>
				))}
			</nav>

			<div className="flex min-h-0 min-w-0 flex-1 overflow-hidden">
				<main
					className={cn(
						"min-w-0 flex-1",
						resolvedTab === "chat"
							? "flex min-h-0 flex-col bg-[var(--leros-surface)]"
							: resolvedTab === "files" || resolvedTab === "activity"
								? "min-h-0 overflow-hidden bg-[var(--leros-surface)]"
								: "overflow-y-auto px-10 py-8",
					)}
				>
					{resolvedTab === "chat" && (
						<ProjectChat layoutMode={projectChatLayoutMode} navigation={navigation} />
					)}
					{resolvedTab === "tasks" && (
						<ProjectTasks tasks={project.tasks} onOpenTask={handleOpenTask} />
					)}
					{resolvedTab === "files" && resolvedProjectId && (
						<ProjectFiles projectId={resolvedProjectId} />
					)}
					{resolvedTab === "activity" && resolvedProjectId && (
						<ProjectActivityPanel
							projectId={resolvedProjectId}
							humanMembers={project.members.filter((member) => member.type === "user")}
							refreshKey={activityRefreshKey}
						/>
					)}
				</main>

				{showProjectSidebar && (
					<div className="flex w-14 shrink-0 items-start justify-center pt-6">
						<button
							type="button"
							className="inline-flex size-10 items-center justify-center rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-[var(--leros-text-muted)] shadow-sm transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
							aria-label={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
							aria-expanded={!rightSidebarCollapsed}
							title={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
							onClick={() => setRightSidebarCollapsed((collapsed) => !collapsed)}
						>
							{rightSidebarCollapsed ? (
								<ChevronsLeft className="size-4" />
							) : (
								<ChevronsRight className="size-4" />
							)}
						</button>
					</div>
				)}

				{showProjectSidebar && !rightSidebarCollapsed && (
					<aside
						className="relative flex min-h-0 shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-6 transition-[width] duration-200 ease-out"
						style={rightSidebarWidthStyle}
					>
						<ProjectConfigSidebar
							project={project}
							compact={!isWideRightSidebar}
							onUpdateProject={async (params) => {
								const updated = await updateProject(params);
								if (!updated) return null;
								// 中文注释：更新项目后权限缓存会失效，立即重拉，避免添加成员和技能入口消失。
								await useAppStore
									.getState()
									.ensureCapabilities(buildProjectCapabilityItems(project.id));
								if (params.members && project.id) {
									await fetchProjectDetail(project.id);
								}
								if (params.members || params.metadata) {
									setActivityRefreshKey((key) => key + 1);
								}
								return updated;
							}}
							onQuickUpdateProjectMembers={async (params, localMembers) => {
								const updated = await updateProjectMembersStore(params, localMembers);
								if (updated) {
									setActivityRefreshKey((key) => key + 1);
								}
								return updated;
							}}
						/>
						<hr
							className={cn(
								"absolute left-0 top-0 z-10 h-full -translate-x-1/2 border-0",
								"w-3 cursor-col-resize",
							)}
							tabIndex={0}
							aria-orientation="vertical"
							aria-label="调整右侧栏宽度"
							aria-valuemin={PROJECT_RIGHT_SIDEBAR_MIN_WIDTH}
							aria-valuemax={PROJECT_RIGHT_SIDEBAR_MAX_WIDTH}
							aria-valuenow={rightSidebarWidth}
							onPointerDown={handleRightSidebarResizeStart}
							onKeyDown={(event) => {
								if (event.key === "ArrowLeft") {
									setRightSidebarWidth(clampProjectRightSidebarWidth(rightSidebarWidth + 8));
								}
								if (event.key === "ArrowRight") {
									setRightSidebarWidth(clampProjectRightSidebarWidth(rightSidebarWidth - 8));
								}
							}}
						/>
					</aside>
				)}
			</div>
		</div>
	);
}

function ProjectChat({
	layoutMode,
	navigation,
}: {
	layoutMode: ProjectChatLayoutMode;
	navigation?: AppNavigation;
}) {
	const layout = getProjectChatLayoutClasses(layoutMode);

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div className="no-scrollbar min-h-0 flex-1 overflow-y-auto">
				<ProjectEmptyState layout={layout} />
			</div>
			<ChatInput variant="project" projectLayoutMode={layoutMode} navigation={navigation} />
		</div>
	);
}

function ProjectConfigSidebar({
	project,
	compact,
	onUpdateProject,
	onQuickUpdateProjectMembers,
}: {
	project: Project;
	compact: boolean;
	onUpdateProject: (params: {
		public_id: string;
		name?: string;
		description?: string;
		status?: string;
		owner_id?: number;
		members?: { type: "assistant" | "user"; id: string }[];
		metadata?: Record<string, unknown>;
	}) => Promise<Project | null>;
	onQuickUpdateProjectMembers: (
		params: {
			public_id: string;
			members: { type: "assistant" | "user"; id: string }[];
		},
		localMembers: ProjectMember[],
	) => Promise<Project | null>;
}) {
	const { user } = useAuth();
	useProjectCapabilities(project.id);
	const { allowed: canDeleteProjectMember } = useCan(
		Action.ProjectMemberDelete,
		{ type: "project", publicId: project.id },
		false,
	);
	const [editingDescription, setEditingDescription] = useState(false);
	const [descriptionDraft, setDescriptionDraft] = useState(project.description);
	const [savingDescription, setSavingDescription] = useState(false);
	const [memberDialogOpen, setMemberDialogOpen] = useState(false);
	const [savingMembers, setSavingMembers] = useState(false);
	const [savingSkills, setSavingSkills] = useState(false);
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [projectSkills, setProjectSkills] = useState<ProjectSkill[]>(project.skills);
	const [projectMCPs, setProjectMCPs] = useState<PluginListItem[]>([]);
	const [mcpOptions, setMCPOptions] = useState<PluginListItem[]>([]);
	const [mcpOpen, setMCPOpen] = useState(false);
	const [mcpSearch, setMCPSearch] = useState("");
	const [savingMCPs, setSavingMCPs] = useState(false);
	const [mcpsLoading, setMCPsLoading] = useState(false);
	const { assistants, assistantsLoaded, fetchAssistants } = useDAStore((s) => s);
	const { skillOptions, skillsLoading, skillsError, reloadSkillOptions } = useSkillPickerOptions({
		projectId: project.id,
		includeBuiltin: false,
		enabled: skillOpen,
	});

	useEffect(() => {
		if (!editingDescription) {
			setDescriptionDraft(project.description);
		}
	}, [editingDescription, project.description]);

	useEffect(() => {
		if (assistantsLoaded) return;
		void fetchAssistants();
	}, [assistantsLoaded, fetchAssistants]);

	useEffect(() => {
		let cancelled = false;
		pluginApi
			.listProject({ public_id: project.id, kind: "skill" })
			.then((response) => {
				if (!cancelled) setProjectSkills(response.data.data.map(pluginToProjectSkill));
			})
			.catch(() => {
				if (!cancelled) setProjectSkills([]);
			});
		return () => {
			cancelled = true;
		};
	}, [project.id]);

	const reloadProjectMCPs = useCallback(async () => {
		setMCPsLoading(true);
		try {
			const [organizationResponse, projectResponse] = await Promise.all([
				pluginApi.list({ kind: "mcp", status: "active", limit: 100 }),
				pluginApi.listProject({ public_id: project.id, kind: "mcp" }),
			]);
			setMCPOptions(organizationResponse.data.data.plugins ?? []);
			setProjectMCPs(projectResponse.data.data ?? []);
		} catch {
			setMCPOptions([]);
			setProjectMCPs([]);
		} finally {
			setMCPsLoading(false);
		}
	}, [project.id]);

	useEffect(() => {
		void reloadProjectMCPs();
	}, [reloadProjectMCPs]);

	const selectedSkillCodes = useMemo(
		() => projectSkills.map((skill) => skill.code),
		[projectSkills],
	);
	const selectedSkillCodeSet = useMemo(
		() => new Set(selectedSkillCodes.map((code) => code.toLowerCase())),
		[selectedSkillCodes],
	);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return (skillOptions ?? []).filter((skill) => {
			if (!query) return true;
			return [skill.label, skill.code, skill.description].join(" ").toLowerCase().includes(query);
		});
	}, [skillOptions, skillSearch]);
	const selectedMCPIDs = useMemo(
		() => new Set(projectMCPs.map((connector) => connector.public_id)),
		[projectMCPs],
	);
	const filteredMCPs = useMemo(() => {
		const query = mcpSearch.trim().toLowerCase();
		return mcpOptions.filter((connector) => {
			if (selectedMCPIDs.has(connector.public_id)) return false;
			if (!query) return true;
			return [connector.name, connector.code, connector.description]
				.filter(Boolean)
				.join(" ")
				.toLowerCase()
				.includes(query);
		});
	}, [mcpOptions, mcpSearch, selectedMCPIDs]);
	const projectMembersWithLatestAssistantAvatar = useMemo(
		() =>
			project.members.map((member) => {
				if (member.type !== "assistant") return member;
				const matchedAssistant = assistants.find(
					(assistant) =>
						(member.publicId && assistant.publicId === member.publicId) ||
						(member.memberId > 0 && assistant.id === member.memberId),
				);
				if (!matchedAssistant) return member;

				return {
					...member,
					name: member.name || matchedAssistant.name,
					roleName: matchedAssistant.roleName,
					description: member.description || matchedAssistant.description,
					// 中文注释：项目详情里的成员头像可能是旧快照，优先用最新 AI 队友头像 public_id。
					avatarUrl: matchedAssistant.avatar || member.avatarUrl,
				};
			}),
		[assistants, project.members],
	);

	const saveDescription = async () => {
		const nextDescription = descriptionDraft.trim();
		setSavingDescription(true);
		try {
			const updated = await onUpdateProject({
				public_id: project.id,
				description: nextDescription,
			});
			if (updated) {
				setEditingDescription(false);
				toast.success("项目描述已更新");
			}
		} finally {
			setSavingDescription(false);
		}
	};

	const addProjectSkill = async (skill: PluginComposerOption) => {
		if (
			savingSkills ||
			skill.projectAssociated ||
			selectedSkillCodeSet.has(skill.code.toLowerCase())
		) {
			return;
		}
		setSavingSkills(true);
		let installedDuringAction = false;
		try {
			const resolved = await bindSkillToProject(project.id, skill);
			installedDuringAction = resolved.installedDuringAction;
			setProjectSkills((current) => [
				...current,
				composerOptionToProjectSkill(skill, resolved.pluginId),
			]);
			await reloadSkillOptions();
			toast.success("项目技能已添加");
		} catch (error) {
			if (error instanceof ProjectSkillBindingError) {
				installedDuringAction = error.installedDuringAction;
			}
			const message = error instanceof Error ? error.message : "项目技能添加失败";
			toast.error(installedDuringAction ? "技能已安装，但项目关联失败" : message);
			if (installedDuringAction) {
				await reloadSkillOptions();
			}
		} finally {
			setSavingSkills(false);
		}
	};

	const removeProjectSkill = async (skill: ProjectSkill) => {
		if (savingSkills || !skill.publicId) return;
		setSavingSkills(true);
		try {
			await pluginApi.removeFromProject({ public_id: project.id, plugin_id: skill.publicId });
			setProjectSkills((current) => current.filter((item) => item.publicId !== skill.publicId));
			toast.success("项目技能已移除");
		} finally {
			setSavingSkills(false);
		}
	};
	const addProjectMCP = async (connector: PluginListItem) => {
		if (savingMCPs || selectedMCPIDs.has(connector.public_id)) return;
		setSavingMCPs(true);
		try {
			await pluginApi.addToProject({
				public_id: project.id,
				plugin_id: connector.public_id,
			});
			setProjectMCPs((current) => [...current, connector]);
			toast.success("MCP 连接器已关联");
		} catch (requestError) {
			toast.error(requestError instanceof Error ? requestError.message : "MCP 连接器关联失败");
		} finally {
			setSavingMCPs(false);
		}
	};
	const removeProjectMCP = async (connector: PluginListItem) => {
		if (savingMCPs) return;
		setSavingMCPs(true);
		try {
			await pluginApi.removeFromProject({
				public_id: project.id,
				plugin_id: connector.public_id,
			});
			setProjectMCPs((current) => current.filter((item) => item.public_id !== connector.public_id));
			toast.success("MCP 连接器已移除");
		} catch (requestError) {
			toast.error(requestError instanceof Error ? requestError.message : "MCP 连接器移除失败");
		} finally {
			setSavingMCPs(false);
		}
	};
	const visibleProjectMembers = useMemo(
		() =>
			sortProjectMembers(
				projectMembersWithLatestAssistantAvatar.filter(
					// 中文注释：默认 AI 员工只作为系统兜底分配，不在右侧项目队友展示区占位。
					(member) =>
						!(
							member.type === "assistant" &&
							(member.isDefault || isSystemDefaultAssistant(member.publicId))
						),
				),
			),
		[projectMembersWithLatestAssistantAvatar],
	);
	const resolveProjectMembersForUpdate = async (nextMembers: ProjectMember[]) => {
		let resolvedMembers = nextMembers.map((member) => ({ ...member }));
		const needAssistantPublicIds = resolvedMembers.some(
			(member) => member.type === "assistant" && !member.isDefault && !member.publicId,
		);
		if (needAssistantPublicIds && !assistantsLoaded) {
			await fetchAssistants();
		}

		const latestAssistants = useAppStore.getState().assistants;
		const assistantPublicIdByMemberId = new Map(
			latestAssistants.map((assistant) => [assistant.id, assistant.publicId]),
		);
		resolvedMembers = resolvedMembers.map((member) => {
			if (member.type !== "assistant" || member.publicId) return member;
			const publicId = assistantPublicIdByMemberId.get(member.memberId);
			return publicId ? { ...member, publicId, id: `assistant-${publicId}` } : member;
		});

		const needUserPublicIds = resolvedMembers.some(
			(member) => member.type === "user" && member.role !== "owner" && !member.publicId,
		);
		if (needUserPublicIds) {
			const unresolvedUsers = resolvedMembers.filter(
				(member) => member.type === "user" && member.role !== "owner" && !member.publicId,
			);
			const userPublicIdByName = new Map<string, string>();
			await Promise.all(
				unresolvedUsers.map(async (member) => {
					const users = await projectMemberApi.listHumanMembers({
						keyword: member.name,
						limit: 20,
					});
					const exactMatches = users.filter((user) => user.name === member.name);
					// 中文注释：ListUsers 不返回内部 uin，只能在姓名唯一命中时回填 public_id，避免误保留错误成员。
					const matchedUser = exactMatches[0];
					if (exactMatches.length === 1 && matchedUser) {
						userPublicIdByName.set(member.name, matchedUser.public_id);
					}
				}),
			);
			resolvedMembers = resolvedMembers.map((member) => {
				if (member.type !== "user" || member.publicId) return member;
				const publicId = userPublicIdByName.get(member.name);
				return publicId ? { ...member, publicId, id: `user-${publicId}` } : member;
			});
		}

		const unresolvedMembers = resolvedMembers.filter(
			(member) =>
				!member.publicId &&
				!(member.type === "assistant" && member.isDefault) &&
				member.role !== "owner",
		);
		if (unresolvedMembers.length > 0) {
			throw new Error("成员身份解析失败，请稍后重试");
		}

		return resolvedMembers;
	};

	const updateProjectMembers = async (
		nextMembers: ProjectMember[],
		options?: { quickRemoval?: boolean },
	) => {
		setSavingMembers(true);
		try {
			const resolvedMembers = await resolveProjectMembersForUpdate(nextMembers);
			const params = {
				public_id: project.id,
				members: projectMembersToInputs(resolvedMembers),
			};
			const updated = options?.quickRemoval
				? await onQuickUpdateProjectMembers(params, resolvedMembers)
				: await onUpdateProject(params);
			if (!updated) {
				throw new Error("项目队友更新失败");
			}
			toast.success("项目队友已更新");
		} catch (error) {
			const message = error instanceof Error ? error.message : "项目队友更新失败";
			toast.error(message);
		} finally {
			setSavingMembers(false);
		}
	};

	const removeProjectMember = (memberToRemove: ProjectMember) => {
		if (savingMembers || !canQuickRemoveProjectMember(memberToRemove, user?.publicId)) return;
		void updateProjectMembers(
			project.members.filter((member) => !isSameProjectMember(member, memberToRemove)),
			{ quickRemoval: true },
		);
	};

	return (
		<div className="no-scrollbar min-h-0 flex-1 space-y-7 overflow-y-auto pr-1">
			<div>
				<p className="text-sm font-semibold text-[var(--leros-text-strong)]">项目配置</p>
				<p className="mt-1 text-xs text-[var(--leros-text-muted)]">管理项目描述和可用技能</p>
			</div>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">项目描述</h2>
					{!editingDescription && (
						<CanGate
							action={Action.ProjectUpdate}
							resource={{ type: "project", publicId: project.id }}
						>
							<button
								type="button"
								className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
								aria-label="编辑项目描述"
								onClick={() => setEditingDescription(true)}
							>
								<Pencil className="size-3.5" />
							</button>
						</CanGate>
					)}
				</div>
				<div
					className={cn(
						"rounded-xl border border-[var(--leros-control-border)] bg-white p-4",
						compact && "px-3",
					)}
				>
					{editingDescription ? (
						<div className="space-y-3">
							<div className="relative">
								<textarea
									value={descriptionDraft}
									onChange={(event) => setDescriptionDraft(event.target.value)}
									placeholder="补充项目目标、背景或协作范围"
									maxLength={500}
									className="min-h-28 w-full resize-none rounded-lg border border-[var(--leros-control-border)] bg-white px-3 py-2 pb-7 pr-16 text-sm leading-6 text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:outline-none"
								/>
								<span className="pointer-events-none absolute bottom-2 right-3 text-xs text-[var(--leros-text-subtle)]">
									{descriptionDraft.length}/500
								</span>
							</div>
							<div className="flex justify-end gap-2">
								<button
									type="button"
									className="rounded-md px-3 py-1.5 text-sm text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-surface-soft)]"
									onClick={() => {
										setDescriptionDraft(project.description);
										setEditingDescription(false);
									}}
									disabled={savingDescription}
								>
									取消
								</button>
								<button
									type="button"
									className="rounded-md bg-[var(--leros-primary)] px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-[var(--leros-primary)]/90 disabled:cursor-not-allowed disabled:opacity-50"
									onClick={saveDescription}
									disabled={savingDescription}
								>
									确定
								</button>
							</div>
						</div>
					) : (
						<p className="whitespace-pre-wrap text-sm leading-6 text-[var(--leros-text-muted)]">
							{project.description || "暂无项目描述"}
						</p>
					)}
				</div>
			</section>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<div className="flex items-center gap-2">
						<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">项目队友</h2>
						<span className="text-xs text-[var(--leros-text-subtle)]">
							{visibleProjectMembers.length}
						</span>
					</div>
					<CanGate
						action={Action.ProjectMemberCreate}
						resource={{ type: "project", publicId: project.id }}
					>
						<button
							type="button"
							className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
							aria-label="添加项目队友"
							onClick={() => setMemberDialogOpen(true)}
							disabled={savingMembers}
						>
							<Plus className="size-4" />
						</button>
					</CanGate>
				</div>
				<div className="no-scrollbar max-h-[320px] overflow-y-auto rounded-xl border border-[var(--leros-control-border)] bg-white p-3">
					{visibleProjectMembers.length === 0 ? (
						<p className="px-3 py-4 text-center text-xs text-[var(--leros-text-subtle)]">
							暂无项目队友
						</p>
					) : (
						<div className={projectMemberListClassName}>
							{visibleProjectMembers.map((member) => {
								const canQuickRemove = canQuickRemoveProjectMember(member, user?.publicId);
								return (
									<ProjectMemberChip
										key={member.id}
										member={member}
										readonly={!canQuickRemove}
										canRemove={canDeleteProjectMember && !savingMembers}
										onRemove={() => removeProjectMember(member)}
										className={projectMemberChipClassName}
									/>
								);
							})}
						</div>
					)}
				</div>
				<ProjectMemberPickerDialog
					open={memberDialogOpen}
					onOpenChange={setMemberDialogOpen}
					selectedMembers={projectMembersWithLatestAssistantAvatar}
					onConfirm={(members) => {
						// 中文注释：成员弹窗提交完整草稿，确保新增、删除和身份修改都能同步到项目。
						void updateProjectMembers(members);
					}}
				/>
			</section>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<div className="flex items-center gap-2">
						<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">技能</h2>
						<span className="text-xs text-[var(--leros-text-subtle)]">{projectSkills.length}</span>
					</div>
					<CanGate
						action={Action.ProjectUpdate}
						resource={{ type: "project", publicId: project.id }}
					>
						<Popover
							open={skillOpen}
							onOpenChange={(open) => {
								setSkillOpen(open);
								if (!open) {
									setSkillSearch("");
								}
							}}
						>
							<PopoverTrigger
								type="button"
								className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
								aria-label="添加技能"
							>
								<Plus className="size-4" />
							</PopoverTrigger>
							{/* 固定在按钮上方，和输入框工具栏的技能选择弹窗保持一致。 */}
							<PopoverContent
								align="end"
								side="top"
								sideOffset={10}
								collisionAvoidance={{
									side: "none",
									align: "shift",
									fallbackAxisSide: "none",
								}}
								className="w-[340px] p-1.5"
							>
								<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
									<div className="px-2 py-1 text-sm font-semibold text-slate-800">选择技能</div>
									<CommandInput
										value={skillSearch}
										onValueChange={setSkillSearch}
										placeholder="搜索技能"
										className="placeholder:text-slate-300"
									/>
									<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
									<CommandList className="max-h-64 px-1">
										<CommandEmpty className="py-6 text-slate-400">
											没有可继续添加的技能
										</CommandEmpty>
										<CommandGroup className="p-0">
											{skillsLoading && (
												<div className="px-2 py-1.5 text-xs text-slate-400">技能加载中...</div>
											)}
											{!skillsLoading && skillsError && (
												<div className="px-2 py-1.5 text-xs text-red-400">{skillsError}</div>
											)}
											{filteredSkills.map((skill) => (
												<CommandItem
													key={skill.code}
													value={skill.label}
													disabled={
														skill.projectAssociated ||
														selectedSkillCodeSet.has(skill.code.toLowerCase())
													}
													onSelect={() => void addProjectSkill(skill)}
													className="rounded-lg px-2 py-1.5"
												>
													<SkillPickerIcon />
													<div className="min-w-0 flex-1">
														<div className="truncate font-medium">
															{renderHighlightedText(skill.label, skillSearch)}
														</div>
														<div className="truncate text-xs text-slate-400">
															{renderHighlightedText(
																skill.description || "项目可用技能",
																skillSearch,
															)}
														</div>
													</div>
												</CommandItem>
											))}
										</CommandGroup>
									</CommandList>
								</Command>
							</PopoverContent>
						</Popover>
					</CanGate>
				</div>
				<div className="no-scrollbar max-h-[280px] overflow-y-auto rounded-xl border border-[var(--leros-control-border)] bg-white p-4">
					{projectSkills.length === 0 ? (
						<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-3 py-4 text-center text-xs text-[var(--leros-text-subtle)]">
							暂无技能
						</div>
					) : (
						<div className="flex flex-wrap gap-2">
							{projectSkills.map((skill) => (
								<div
									key={skill.publicId ?? skill.code}
									className="group inline-flex items-center gap-2 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] py-1.5 pl-1.5 pr-2"
								>
									<CanGate
										action={Action.ProjectUpdate}
										resource={{ type: "project", publicId: project.id }}
										// 中文注释：成员无权更新项目技能时，仅展示图标，避免暴露点击后必然失败的移除入口。
										fallback={<SkillPickerIcon />}
									>
										<button
											type="button"
											className="relative flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600 disabled:cursor-not-allowed disabled:opacity-50"
											aria-label={`移除技能 ${skill.name}`}
											onClick={() => void removeProjectSkill(skill)}
											disabled={savingSkills}
										>
											<Sparkles className="size-3.5 transition-opacity group-hover:opacity-0" />
											<span className="absolute inline-flex items-center justify-center rounded-full p-0.5 text-[var(--leros-text-subtle)] opacity-0 transition-opacity hover:bg-[var(--leros-control-border)] hover:text-[var(--leros-text)] group-hover:opacity-100">
												<X className="size-3" />
											</span>
										</button>
									</CanGate>
									<span className="max-w-[140px] truncate text-xs font-medium text-[var(--leros-text)]">
										{skill.name}
									</span>
								</div>
							))}
						</div>
					)}
				</div>
			</section>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<div className="flex items-center gap-2">
						<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">MCP 连接器</h2>
						<span className="text-xs text-[var(--leros-text-subtle)]">{projectMCPs.length}</span>
					</div>
					<CanGate
						action={Action.ProjectUpdate}
						resource={{ type: "project", publicId: project.id }}
					>
						<Popover
							open={mcpOpen}
							onOpenChange={(open) => {
								setMCPOpen(open);
								if (!open) setMCPSearch("");
							}}
						>
							<PopoverTrigger
								type="button"
								className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
								aria-label="添加 MCP 连接器"
							>
								<Plus className="size-4" />
							</PopoverTrigger>
							<PopoverContent align="end" side="top" sideOffset={10} className="w-[340px] p-1.5">
								<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
									<div className="px-2 py-1 text-sm font-semibold text-slate-800">
										选择 MCP 连接器
									</div>
									<CommandInput
										value={mcpSearch}
										onValueChange={setMCPSearch}
										placeholder="搜索 MCP 连接器"
										className="placeholder:text-slate-300"
									/>
									<CommandSeparator className="mx-1 my-2 bg-slate-200/80" />
									<CommandList className="max-h-64 px-1">
										<CommandEmpty className="py-6 text-slate-400">
											没有可继续添加的 MCP 连接器
										</CommandEmpty>
										<CommandGroup className="p-0">
											{mcpsLoading && (
												<div className="px-2 py-1.5 text-xs text-slate-400">加载中...</div>
											)}
											{filteredMCPs.map((connector) => (
												<CommandItem
													key={connector.public_id}
													value={connector.name}
													disabled={savingMCPs}
													onSelect={() => void addProjectMCP(connector)}
													className="rounded-lg px-2 py-1.5"
												>
													<MCPConnectorIcon code={connector.code} name={connector.name} />
													<div className="min-w-0 flex-1">
														<div className="truncate font-medium">{connector.name}</div>
														<div className="truncate text-xs text-slate-400">
															{connector.description || connector.code}
														</div>
													</div>
												</CommandItem>
											))}
										</CommandGroup>
									</CommandList>
								</Command>
							</PopoverContent>
						</Popover>
					</CanGate>
				</div>
				<div className="no-scrollbar max-h-[220px] overflow-y-auto rounded-xl border border-[var(--leros-control-border)] bg-white p-4">
					{projectMCPs.length === 0 ? (
						<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-3 py-4 text-center text-xs text-[var(--leros-text-subtle)]">
							暂无 MCP 连接器
						</div>
					) : (
						<div className="flex flex-wrap gap-2">
							{projectMCPs.map((connector) => (
								<div
									key={connector.public_id}
									className="group inline-flex items-center gap-2 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] py-1.5 pl-1.5 pr-2"
								>
									<CanGate
										action={Action.ProjectUpdate}
										resource={{ type: "project", publicId: project.id }}
										fallback={<MCPConnectorIcon code={connector.code} name={connector.name} />}
									>
										<button
											type="button"
											className="relative flex size-7 items-center justify-center rounded-lg bg-blue-50 text-blue-600 disabled:opacity-50"
											aria-label={`移除 MCP 连接器 ${connector.name}`}
											onClick={() => void removeProjectMCP(connector)}
											disabled={savingMCPs}
										>
											<MCPConnectorIcon
												code={connector.code}
												name={connector.name}
												className="transition-opacity group-hover:opacity-0"
											/>
											<X className="absolute size-3 opacity-0 transition-opacity group-hover:opacity-100" />
										</button>
									</CanGate>
									<span className="max-w-[140px] truncate text-xs font-medium">
										{connector.name}
									</span>
								</div>
							))}
						</div>
					)}
				</div>
			</section>
		</div>
	);
}

/** 与输入框「添加技能」弹窗保持一致的技能图标样式 */
function SkillPickerIcon() {
	return (
		<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
			<Sparkles className="size-3.5" />
		</div>
	);
}

function pluginToProjectSkill(plugin: PluginListItem): ProjectSkill {
	return {
		publicId: plugin.public_id,
		code: plugin.code,
		name: plugin.name,
		description: plugin.description,
		category: plugin.kind,
		source: "organization",
	};
}

function composerOptionToProjectSkill(
	option: PluginComposerOption,
	pluginId: string,
): ProjectSkill {
	return {
		publicId: pluginId,
		code: option.code,
		name: option.label,
		description: option.description,
		category: "skill",
		source: "organization",
	};
}

function ProjectEmptyState({ layout }: { layout: ReturnType<typeof getProjectChatLayoutClasses> }) {
	return (
		<div className={cn("flex h-full", layout.shell)}>
			<div className={cn(layout.inner, "flex h-full flex-col justify-center py-16")}>
				<div className="flex items-center gap-5 text-left md:gap-6">
					<div className="leros-workbench-hero-icon shrink-0">
						<img
							src={PROJECT_NEW_TASK_HERO_OCTOPUS_SRC}
							alt=""
							className="size-50 object-contain"
						/>
					</div>
					<div className="flex min-w-0 flex-col gap-8">
						<h2 className="text-4xl tracking-tight text-[var(--leros-primary)] md:text-5xl">
							开始新任务
						</h2>
						<p className="text-lg text-[var(--leros-text-muted)]">
							描述需求或目标，Lework 将自动规划、执行、交付，并将产物归档到项目中
						</p>
					</div>
				</div>
			</div>
		</div>
	);
}

function ProjectTasks({
	tasks,
	onOpenTask,
}: {
	tasks: ProjectTask[];
	onOpenTask?: (task: ProjectTask) => void;
}) {
	const { updateTask } = useLayoutStore((s) => s);
	const taskCapabilityItems = useMemo(
		() => tasks.flatMap((task) => buildTaskCapabilityItems(task.id)),
		[tasks],
	);
	useEnsureCapabilities(taskCapabilityItems, tasks.length > 0);
	const [renameTarget, setRenameTarget] = useState<ProjectTask | null>(null);
	const [renameValue, setRenameValue] = useState("");
	const [deleteTarget, setDeleteTarget] = useState<ProjectTask | null>(null);

	const handleConfirmRename = async () => {
		const title = renameValue.trim();
		if (!renameTarget || !title) return;

		const updatedTask = await updateTask({ public_id: renameTarget.id, title });
		if (updatedTask) {
			setRenameTarget(null);
			setRenameValue("");
		}
	};

	return (
		// 中文注释：任务 tab 需要占用更宽的主内容区域，避免大屏下卡片挤在中间留下过多留白。
		<div className="mx-auto w-full max-w-[1100px]">
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">任务列表</h2>
			<div className="mt-4">
				<ProjectTaskList
					tasks={tasks}
					onRename={(task) => {
						setRenameTarget(task);
						setRenameValue(task.title);
					}}
					onDelete={setDeleteTarget}
					onOpen={onOpenTask}
				/>
			</div>
			<Dialog open={renameTarget !== null} onOpenChange={(open) => !open && setRenameTarget(null)}>
				<DialogContent className="sm:max-w-md" showCloseButton={false}>
					<DialogHeader>
						<DialogTitle>重命名任务</DialogTitle>
						<DialogDescription>请输入新的任务名称</DialogDescription>
					</DialogHeader>
					<div className="mt-4">
						<input
							type="text"
							value={renameValue}
							onChange={(event) => setRenameValue(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter") {
									handleConfirmRename();
								}
							}}
							placeholder="任务名称"
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
						/>
					</div>
					<DialogFooter className="mt-4">
						<Button variant="outline" onClick={() => setRenameTarget(null)}>
							取消
						</Button>
						<Button onClick={handleConfirmRename} disabled={!renameValue.trim()}>
							确认
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
			{deleteTarget && (
				<TaskDeleteDialog
					task={deleteTarget}
					open={true}
					onOpenChange={(open) => {
						if (!open) setDeleteTarget(null);
					}}
				/>
			)}
		</div>
	);
}

function TaskInlineActions({
	task,
	onRename,
	onDelete,
}: {
	task: ProjectTask;
	onRename?: (task: ProjectTask) => void;
	onDelete?: (task: ProjectTask) => void;
}) {
	const resource = { type: "task" as const, publicId: task.id };

	return (
		<>
			{onRename ? (
				<CanGate action={Action.TaskUpdate} resource={resource}>
					<button
						type="button"
						className="rounded p-0.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
						onClick={(event) => {
							event.stopPropagation();
							onRename(task);
						}}
						title="重命名任务"
					>
						<Pencil className="size-4" />
					</button>
				</CanGate>
			) : null}
			{onDelete ? (
				<CanGate action={Action.TaskDelete} resource={resource}>
					<button
						type="button"
						className="rounded p-0.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-danger-softer)] hover:text-[var(--leros-danger)]"
						onClick={(event) => {
							event.stopPropagation();
							onDelete(task);
						}}
						title="删除任务"
					>
						<Trash2 className="size-4" />
					</button>
				</CanGate>
			) : null}
		</>
	);
}

function ProjectTaskList({
	tasks,
	compact = false,
	onRename,
	onDelete,
	onOpen,
}: {
	tasks: ProjectTask[];
	compact?: boolean;
	onRename?: (task: ProjectTask) => void;
	onDelete?: (task: ProjectTask) => void;
	onOpen?: (task: ProjectTask) => void;
}) {
	if (tasks.length === 0) {
		return (
			<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]">
				暂无任务
			</div>
		);
	}

	return (
		<div className={cn("w-full", compact && "mx-auto max-w-[250px]")}>
			<div className={cn(compact ? SIDEBAR_COMPACT_LIST_CLASS : "space-y-3")}>
				{tasks.map((task) => {
					const cardClassName = cn(
						"group relative w-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] shadow-sm",
						onOpen &&
							"cursor-pointer transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35",
						"rounded-lg",
					);
					const contentClassName = cn(
						"flex w-full min-w-0 items-start text-left",
						compact ? "gap-3 px-3.5 py-3" : "gap-3.5 px-4 py-3.5",
					);
					const content = (
						<>
							<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								{/* 主列表和右侧列表统一使用固定任务图标，避免状态图标和语义图标混用。 */}
								<TaskCardIcon className="size-5" />
							</div>
							<div className="min-w-0 flex-1 text-left">
								<div
									className={cn(
										"text-sm font-normal leading-5 text-[var(--leros-text-strong)]",
										"line-clamp-2",
									)}
								>
									{task.title}
								</div>
							</div>
						</>
					);

					if (!onDelete) {
						return (
							<button
								key={task.id}
								type="button"
								className={cn(cardClassName, contentClassName)}
								onClick={() => onOpen?.(task)}
								disabled={!onOpen}
								title={onOpen ? "打开任务会话" : undefined}
							>
								{content}
							</button>
						);
					}

					return (
						<div key={task.id} className={cardClassName}>
							<button
								type="button"
								className={cn(contentClassName, "pr-24")}
								onClick={() => onOpen?.(task)}
								disabled={!onOpen}
								title={onOpen ? "打开任务会话" : undefined}
							>
								{content}
							</button>
							{!compact && (onRename || onDelete) ? (
								<div className="pointer-events-none absolute right-4 top-4 z-10 flex items-center gap-1 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100">
									<TaskInlineActions task={task} onRename={onRename} onDelete={onDelete} />
								</div>
							) : null}
						</div>
					);
				})}
			</div>
		</div>
	);
}

type ProjectFileQueryFilters = {
	source: ProjectFileSourceFilter;
	type: ProjectFileTypeFilter;
};

function ProjectFiles({ projectId }: { projectId: string }) {
	const [uploading] = useState(false);
	const [uploadError] = useState<string | null>(null);
	const [files, setFiles] = useState<ProjectFileNode[]>([]);
	const [filesLoading, setFilesLoading] = useState(true);
	const [searchKeyword, setSearchKeyword] = useState("");
	const [fileSourceFilter, setFileSourceFilter] = useState<ProjectFileSourceFilter>("all");
	const [fileTypeFilter, setFileTypeFilter] = useState<ProjectFileTypeFilter>("all");
	const [syncedFilters, setSyncedFilters] = useState<ProjectFileQueryFilters>({
		source: "all",
		type: "all",
	});
	const deferredSearchKeyword = useDeferredValue(searchKeyword);

	const fetchFiles = useCallback(async () => {
		const requestProjectId = projectId;
		const requestFilters: ProjectFileQueryFilters = {
			source: fileSourceFilter,
			type: fileTypeFilter,
		};
		setFilesLoading(true);
		try {
			const response = await projectFileApi.list(
				buildProjectFileListParams(requestProjectId, requestFilters.source, requestFilters.type),
			);
			if (
				requestProjectId !== projectId ||
				requestFilters.source !== fileSourceFilter ||
				requestFilters.type !== fileTypeFilter
			) {
				return;
			}
			setFiles(parseProjectFileList(response.data.data));
			setSyncedFilters(requestFilters);
		} catch (err) {
			console.error("ProjectFiles fetch error:", err);
			if (
				requestProjectId === projectId &&
				requestFilters.source === fileSourceFilter &&
				requestFilters.type === fileTypeFilter
			) {
				setFiles([]);
				setSyncedFilters(requestFilters);
			}
		} finally {
			if (
				requestProjectId === projectId &&
				requestFilters.source === fileSourceFilter &&
				requestFilters.type === fileTypeFilter
			) {
				setFilesLoading(false);
			}
		}
	}, [projectId, fileSourceFilter, fileTypeFilter]);

	useEffect(() => {
		void fetchFiles();
	}, [fetchFiles]);

	useEffect(() => {
		const handleRestored = (event: Event) => {
			const detail = (event as CustomEvent<{ projectId?: string }>).detail;
			if (detail?.projectId && detail.projectId !== projectId) return;
			void fetchFiles();
		};
		window.addEventListener(PROJECT_FILE_VERSION_CHANGED_EVENT, handleRestored);
		return () => window.removeEventListener(PROJECT_FILE_VERSION_CHANGED_EVENT, handleRestored);
	}, [projectId, fetchFiles]);

	const pendingFilterFetch =
		syncedFilters.source !== fileSourceFilter || syncedFilters.type !== fileTypeFilter;
	const showFilesLoading = filesLoading || pendingFilterFetch;
	const hasSearch = deferredSearchKeyword.trim().length > 0;
	const isFlatDisplay = hasSearch || isProjectFileFlatDisplay(syncedFilters.type);
	const flatSourceNodes = useMemo(() => {
		if (hasSearch) {
			return getProjectFileSearchSourceNodes(files, syncedFilters.type);
		}
		return collectSelectableFiles(files);
	}, [files, syncedFilters.type, hasSearch]);
	const displayNodes = useMemo(() => {
		if (!isFlatDisplay) {
			return files;
		}
		return filterProjectFileSearchResults(flatSourceNodes, deferredSearchKeyword);
	}, [files, flatSourceNodes, deferredSearchKeyword, isFlatDisplay]);
	const hasVisibleNodes = displayNodes.length > 0;

	// 中文注释：当前 files 页签的上传入口仍处于注释停用状态，先保留实现并显式标记未启用，避免误恢复旧交互。
	// const _handleUpload = async (event: ChangeEvent<HTMLInputElement>) => {
	// 	const file = event.target.files?.[0];
	// 	event.target.value = "";
	// 	if (!file) return;

	// 	setUploading(true);
	// 	setUploadError(null);
	// 	try {
	// 		await projectFileApi.upload({ projectId, file });
	// 		await onRefresh();
	// 		toast.success("文件上传成功");
	// 	} catch (err) {
	// 		setUploadError(err instanceof Error ? err.message : "上传文件失败");
	// 	} finally {
	// 		setUploading(false);
	// 	}
	// };

	const handleDownload = async (file: ProjectFileNode) => {
		try {
			if (file.type === "directory") {
				const blob = await downloadProjectFolderAsZip(projectId, file, files);
				triggerBlobDownload(blob, `${file.name}.zip`);
				return;
			}

			const response = file.storageUri
				? await fetchFilePreviewByStorageUri(file.storageUri)
				: await projectFileApi.fetchDownload(projectId, file.path);
			const blob = await response.blob();
			triggerBlobDownload(blob, file.name);
		} catch (err) {
			const message = err instanceof Error ? err.message : "下载失败";
			console.error("ProjectFiles download error:", err);
			toast.error(message, { position: "bottom-right" });
		}
	};

	return (
		<div className="h-full min-w-0 overflow-auto px-10 py-7">
			<div className="mx-auto w-full min-w-0 max-w-[1200px]">
				<div className="mb-7 flex flex-wrap items-center justify-between gap-4">
					<div className="min-w-0">
						<h2 className="text-[2rem] font-semibold tracking-tight text-[var(--leros-text-strong)]">
							项目文件
						</h2>
						<p className="mt-0.5 text-[13px] text-[var(--leros-text-muted)]">
							管理当前项目的所有文件资源
						</p>
					</div>
					{/* 中文注释：项目文件页顶部筛选条整体收一档，保持结构不变，只降低高度和横向占比，让桌面端视觉更紧凑。 */}
					<div className="flex flex-wrap items-end gap-2.5">
						<div className="flex flex-col gap-1">
							<span className="text-[12px] text-[var(--leros-text-muted)]">搜索</span>
							<div className="relative">
								<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
								<input
									value={searchKeyword}
									onChange={(event) => setSearchKeyword(event.target.value)}
									placeholder="搜索文件..."
									className="h-9 w-60 rounded-xl border border-[var(--leros-control-border)] bg-white pl-9 pr-3.5 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
								/>
							</div>
						</div>
						<div className="flex flex-col gap-1">
							<span className="text-[12px] text-[var(--leros-text-muted)]">来源</span>
							<div className="relative">
								<select
									value={fileSourceFilter}
									onChange={(event) =>
										setFileSourceFilter(event.target.value as ProjectFileSourceFilter)
									}
									className="h-9 min-w-[132px] cursor-pointer appearance-none rounded-xl border border-[var(--leros-control-border)] bg-white py-0 pl-3.5 pr-9 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
								>
									<option value="all">全部</option>
									<option value="task">任务文件</option>
									<option value="upload">上传文件</option>
								</select>
								<ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
							</div>
						</div>
						<div className="flex flex-col gap-1">
							<span className="text-[12px] text-[var(--leros-text-muted)]">类型</span>
							<div className="relative">
								<select
									value={fileTypeFilter}
									onChange={(event) =>
										setFileTypeFilter(event.target.value as ProjectFileTypeFilter)
									}
									className="h-9 min-w-[132px] cursor-pointer appearance-none rounded-xl border border-[var(--leros-control-border)] bg-white py-0 pl-3.5 pr-9 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
								>
									{PROJECT_FILE_TYPE_FILTER_OPTIONS.map((option) => (
										<option key={option.value} value={option.value}>
											{option.label}
										</option>
									))}
								</select>
								<ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
							</div>
						</div>
						{/* 中文注释：当前只隐藏上传按钮入口，保留上传逻辑和状态处理，后续需要恢复展示时可直接取消注释。 */}
						{/* <label className="inline-flex cursor-pointer items-center gap-2 rounded-xl bg-[var(--leros-primary)] px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90">
							<FileText className="size-4" />
							上传
							<input
								type="file"
								className="hidden"
								accept={PROJECT_ATTACHMENT_ACCEPT}
								onChange={handleUpload}
								disabled={uploading}
							/>
						</label> */}
					</div>
				</div>

				{uploading && (
					<div className="mb-4 rounded-xl border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-4 py-3 text-sm text-[var(--leros-text-muted)]">
						正在上传文件...
					</div>
				)}
				{uploadError && (
					<div className="mb-4 rounded-xl border border-[var(--leros-danger)]/20 bg-[var(--leros-danger-softer)] px-4 py-3 text-sm text-[var(--leros-danger)]">
						{uploadError}
					</div>
				)}

				{showFilesLoading ? (
					<div className="flex items-center justify-center gap-2 px-6 py-16 text-sm text-[var(--leros-text-muted)]">
						<LoaderCircle className="size-5 animate-spin" />
						<span>加载文件中...</span>
					</div>
				) : !hasVisibleNodes ? (
					<div className="px-6 py-16 text-center text-sm text-[var(--leros-text-muted)]">
						暂无文件
					</div>
				) : (
					<div className="overflow-x-auto rounded-2xl border border-[var(--leros-control-border)] bg-white">
						<div className={PROJECT_FILE_TABLE_MIN_WIDTH_CLASS}>
							<div
								className={cn(
									PROJECT_FILE_TABLE_GRID_CLASS,
									"border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] py-3.5 text-[11px] font-semibold uppercase tracking-wider text-[var(--leros-text-muted)]",
								)}
							>
								<div className={cn("min-w-0 truncate", PROJECT_FILE_TABLE_LEADING_CELL_CLASS)}>
									名称
								</div>
								<div className="truncate whitespace-nowrap">来源</div>
								<div className="truncate whitespace-nowrap">类型</div>
								<div className="truncate whitespace-nowrap">大小</div>
								<div className="truncate whitespace-nowrap">创建时间</div>
								<div className={PROJECT_FILE_TABLE_ACTIONS_HEADER_CLASS}>操作</div>
							</div>
							<div>
								<ProjectFileTree
									nodes={displayNodes}
									variant="table"
									layout={isFlatDisplay ? "flat" : "tree"}
									showFullPath={isFlatDisplay}
									searchKeyword={hasSearch ? deferredSearchKeyword : ""}
									fullTree={files}
									projectId={projectId}
									onPreview={(file) => openProjectFilePreview(projectId, file)}
									onDownload={handleDownload}
									formatBytes={formatBytes}
									formatTime={formatTime}
								/>
							</div>
						</div>
					</div>
				)}
			</div>
		</div>
	);
}

function formatBytes(size: number): string {
	if (!size) return "-";
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
	return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function formatTime(timestamp: number): string {
	if (!timestamp) return "-";
	// 中文注释：项目文件列表里的 createdAt 已在 parseProjectFileList 中从秒转成毫秒，这里直接按毫秒格式化，避免重复乘 1000 导致年份异常。
	return new Intl.DateTimeFormat("zh-CN", {
		year: "numeric",
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
	}).format(new Date(timestamp));
}

function clampProjectRightSidebarWidth(width: number): number {
	return Math.min(
		PROJECT_RIGHT_SIDEBAR_MAX_WIDTH,
		Math.max(PROJECT_RIGHT_SIDEBAR_MIN_WIDTH, Math.round(width)),
	);
}
