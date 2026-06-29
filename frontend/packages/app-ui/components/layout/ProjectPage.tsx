"use client";

import {
	fetchFilePreviewByStorageUri,
	type Project,
	type ProjectSkill,
	type ProjectTask,
	projectFileApi,
	type SkillInstalledItem,
	skillMarketplaceApi,
	useChatStore,
	useLayoutStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
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
	Bot,
	Check,
	ChevronDown,
	ChevronRight,
	ChevronsLeft,
	ChevronsLeftRightEllipsis,
	ChevronsRight,
	Download,
	Eye,
	FileText,
	LoaderCircle,
	Pencil,
	Plus,
	Search,
	Sparkles,
	Trash2,
	X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { notifyFeatureUnavailable } from "../ai-teammates/feature-unavailable";
import { MessageTimeline } from "../chat/MessageTimeline";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { ChatInput } from "../input/ChatInput";
import type { AppNavigation } from "./LeftRail";
import { getOfficeOpenXmlFormat, type OfficeOpenXmlFormat, OfficePreview } from "./OfficePreview";
import { getProjectChatLayoutClasses, type ProjectChatLayoutMode } from "./project-chat-layout";
import {
	ProjectFileTypeIcon,
	SIDEBAR_COMPACT_LIST_CLASS,
	TaskCardIcon,
} from "./project-file-type-icon";
import {
	collectSelectableFiles,
	type FileSource,
	getFileSource,
	normalizeProjectFileTree,
	type ProjectFileNode,
	sortProjectFilesByUploadedTimeDesc,
} from "./project-files";
import { SpreadsheetPreview } from "./SpreadsheetPreview";
import { TaskDeleteDialog } from "./TaskDeleteDialog";

const projectTabs = [
	{ id: "chat" as const, label: "新建任务" },
	{ id: "tasks" as const, label: "任务" },
	{ id: "files" as const, label: "项目文件" },
];

const FILE_PREVIEW_DRAWER_DEFAULT_WIDTH = 860;
const FILE_PREVIEW_DRAWER_MIN_WIDTH = 720;
const FILE_PREVIEW_DRAWER_MAX_WIDTH = 1200;
const PROJECT_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY = "leros-project-config-right-sidebar-width";
const PROJECT_RIGHT_SIDEBAR_COLLAPSED_STORAGE_KEY = "leros-project-right-sidebar-collapsed";
const PROJECT_RIGHT_SIDEBAR_DEFAULT_WIDTH = 352;
const PROJECT_RIGHT_SIDEBAR_MIN_WIDTH = 300;
const PROJECT_RIGHT_SIDEBAR_MAX_WIDTH = 440;
const PROJECT_RIGHT_SIDEBAR_WIDE_BREAKPOINT = 360;

type ProjectTab = (typeof projectTabs)[number]["id"];

type FilePreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "error"; message: string }
	| { status: "office"; format: OfficeOpenXmlFormat; buffer: ArrayBuffer }
	| { status: "markdown"; content: string }
	| { status: "text"; content: string }
	| { status: "spreadsheet"; buffer: ArrayBuffer }
	| { status: "blob"; url: string; mimeType: string };

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
		projectSessionId,
		projectSessionProjectId,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
		activeTaskDetailProjectId,
		activeTaskDetailSessionId,
		fetchProjects,
		setProjectRoute,
		setActiveProjectTab,
		fetchProjectDetail,
		openTaskDetail,
		updateProject,
	} = useLayoutStore((s) => s);

	const {
		activeSessionId,
		isGenerating,
		pendingBootstrapSessionId,
		setActiveSession,
		loadConversationMessages,
		resetLocalMessages,
	} = useChatStore((s) => s);

	const [projectFiles, setProjectFiles] = useState<ProjectFileNode[]>([]);
	const [rightSidebarWidth, setRightSidebarWidth] = useState(PROJECT_RIGHT_SIDEBAR_DEFAULT_WIDTH);
	const [rightSidebarCollapsed, setRightSidebarCollapsed] = useState(false);
	const hasLoadedRightSidebarPreferenceRef = useRef(false);

	const resolvedProjectId = projectId ?? activeProjectId;
	const resolvedTab = tab ?? activeProjectTab;
	const project =
		projects.find((item) => item.id === resolvedProjectId) ??
		(resolvedProjectId ? undefined : projects[0]);

	const selectedTaskSessionId = useMemo(() => {
		if (!project || activeWorkbenchProjectId !== project.id || !activeWorkbenchTaskId) return null;
		return project.tasks.find((task) => task.id === activeWorkbenchTaskId)?.sessionId ?? null;
	}, [project, activeWorkbenchProjectId, activeWorkbenchTaskId]);

	const currentTaskSessionId =
		activeTaskDetailProjectId === resolvedProjectId ? activeTaskDetailSessionId : null;

	const streamingTaskSessionId =
		isGenerating &&
		activeSessionId &&
		activeTaskDetailProjectId === resolvedProjectId &&
		activeTaskDetailSessionId === activeSessionId
			? activeTaskDetailSessionId
			: null;
	const currentProjectSessionId =
		projectSessionProjectId === resolvedProjectId ? projectSessionId : null;

	const resolvedSessionId =
		streamingTaskSessionId ??
		currentTaskSessionId ??
		selectedTaskSessionId ??
		currentProjectSessionId;

	const handleOpenTask = (task: ProjectTask) => {
		if (!resolvedProjectId) return;
		if (navigation) {
			navigation.goToTaskDetail(resolvedProjectId, task.id, task.sessionId ?? null);
			return;
		}
		openTaskDetail(resolvedProjectId, task.id, task.sessionId ?? null);
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
	}, [resolvedProjectId, fetchProjectDetail, projects.length]);

	const refreshProjectFiles = async () => {
		if (!resolvedProjectId) return;
		const response = await projectFileApi.list({
			projectId: resolvedProjectId,
		});
		setProjectFiles(normalizeProjectFileTree(response.data.data));
	};

	useEffect(() => {
		if (!resolvedProjectId || resolvedTab !== "files") {
			setProjectFiles([]);
			return;
		}

		const currentProjectId = resolvedProjectId;
		let cancelled = false;

		async function fetchFiles() {
			try {
				const response = await projectFileApi.list({
					projectId: currentProjectId,
				});
				if (cancelled) return;
				setProjectFiles(normalizeProjectFileTree(response.data.data));
			} catch (err) {
				if (cancelled) return;
				console.error("ProjectPage fetch project files error:", err);
				setProjectFiles([]);
			}
		}

		fetchFiles();
		return () => {
			cancelled = true;
		};
	}, [resolvedProjectId, resolvedTab]);

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
			resetLocalMessages();
			return;
		}
		const nextSessionId = resolvedSessionId;
		setActiveSession(nextSessionId);
		if (currentView === "taskDetail" && currentTaskSessionId === nextSessionId) return;
		// 项目消息刚创建 session 并准备开流时，跳过这次自动拉历史，避免旧数据覆盖 optimistic 消息。
		if (pendingBootstrapSessionId === nextSessionId) return;
		if (isGenerating && activeSessionId === nextSessionId) return;
		loadConversationMessages(nextSessionId);
	}, [
		resolvedSessionId,
		currentTaskSessionId,
		projectDetailLoading,
		currentView,
		isGenerating,
		pendingBootstrapSessionId,
		activeSessionId,
		setActiveSession,
		loadConversationMessages,
		resetLocalMessages,
	]);

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

	// 中文注释：项目右侧栏只在会话 tab 使用，任务 tab 不展示展开/拖拽能力。
	const showProjectSidebar = resolvedTab === "chat";
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

	if (projectDetailLoading) {
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
		<div data-slot="project-page" className="flex h-full flex-1 flex-col bg-[var(--leros-surface)]">
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
					<h1 className="max-w-[360px] truncate text-sm font-bold text-[var(--leros-text-strong)]">
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

			<div className="relative min-h-0 flex flex-1">
				<main
					className={cn(
						"min-w-0 flex-1",
						resolvedTab === "chat"
							? "flex min-h-0 flex-col bg-[var(--leros-surface)]"
							: resolvedTab === "files"
								? "min-h-0 bg-[var(--leros-surface)]"
								: "overflow-y-auto px-10 py-8",
					)}
				>
					{resolvedTab === "chat" && (
						<ProjectChat
							layoutMode={projectChatLayoutMode}
							navigation={navigation}
							projectId={resolvedProjectId ?? undefined}
						/>
					)}
					{resolvedTab === "tasks" && (
						<ProjectTasks tasks={project.tasks} onOpenTask={handleOpenTask} />
					)}
					{resolvedTab === "files" && resolvedProjectId && (
						<ProjectFiles
							projectId={resolvedProjectId}
							files={projectFiles}
							onRefresh={refreshProjectFiles}
						/>
					)}
				</main>

				{showProjectSidebar && rightSidebarCollapsed && (
					<button
						type="button"
						className="absolute right-6 top-6 z-20 inline-flex size-10 items-center justify-center rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-[var(--leros-text-muted)] shadow-sm transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
						aria-label="展开右侧栏"
						title="展开右侧栏"
						onClick={() => setRightSidebarCollapsed(false)}
					>
						<ChevronsLeft className="size-4" />
					</button>
				)}

				{showProjectSidebar && !rightSidebarCollapsed && (
					<aside
						className="relative flex shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] transition-[width] duration-200 ease-out"
						style={rightSidebarWidthStyle}
					>
						<ProjectConfigSidebar
							project={project}
							compact={!isWideRightSidebar}
							onCollapse={() => setRightSidebarCollapsed(true)}
							onUpdateProject={updateProject}
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
	projectId,
}: {
	layoutMode: ProjectChatLayoutMode;
	navigation?: AppNavigation;
	projectId?: string;
}) {
	const layout = getProjectChatLayoutClasses(layoutMode);

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<MessageTimeline
				emptyState={<ProjectEmptyState layout={layout} />}
				contentShellClassName={layout.shell}
				contentClassName={layout.timelineInner}
				projectId={projectId}
			/>
			<ChatInput variant="project" projectLayoutMode={layoutMode} navigation={navigation} />
		</div>
	);
}

function ProjectConfigSidebar({
	project,
	compact,
	onCollapse,
	onUpdateProject,
}: {
	project: Project;
	compact: boolean;
	onCollapse: () => void;
	onUpdateProject: (params: {
		public_id: string;
		name?: string;
		description?: string;
		status?: string;
		owner_id?: number;
		metadata?: Record<string, unknown>;
	}) => Promise<Project | null>;
}) {
	const [editingDescription, setEditingDescription] = useState(false);
	const [descriptionDraft, setDescriptionDraft] = useState(project.description);
	const [savingDescription, setSavingDescription] = useState(false);
	const [savingSkills, setSavingSkills] = useState(false);
	const [skillOpen, setSkillOpen] = useState(false);
	const [skillSearch, setSkillSearch] = useState("");
	const [skillOptions, setSkillOptions] = useState<ProjectSkill[]>([]);
	const [skillsLoading, setSkillsLoading] = useState(false);
	const [skillsLoaded, setSkillsLoaded] = useState(false);
	const [skillsError, setSkillsError] = useState<string | null>(null);

	useEffect(() => {
		if (!editingDescription) {
			setDescriptionDraft(project.description);
		}
	}, [editingDescription, project.description]);

	useEffect(() => {
		if (!skillOpen || skillsLoaded) return;

		setSkillsLoading(true);
		setSkillsError(null);
		skillMarketplaceApi
			.installed()
			.then((response) => {
				const raw = normalizeInstalledSkillsPayload(response.data);
				setSkillOptions(raw.map(installedSkillToProjectSkill));
				setSkillsLoaded(true);
			})
			.catch((error: unknown) => {
				const message = error instanceof Error ? error.message : "技能加载失败";
				setSkillsError(message);
				setSkillOptions([]);
			})
			.finally(() => {
				setSkillsLoading(false);
			});
	}, [skillOpen, skillsLoaded]);

	const selectedSkillCodes = useMemo(
		() => project.skills.map((skill) => skill.code),
		[project.skills],
	);
	const filteredSkills = useMemo(() => {
		const query = skillSearch.trim().toLowerCase();
		return skillOptions.filter((skill) => {
			if (selectedSkillCodes.includes(skill.code)) return false;
			if (!query) return true;
			// 中文注释：技能弹窗仅按名称搜索。
			return [skill.name, skill.code].join(" ").toLowerCase().includes(query);
		});
	}, [selectedSkillCodes, skillOptions, skillSearch]);

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

	const updateProjectSkills = async (nextSkills: ProjectSkill[]) => {
		setSavingSkills(true);
		try {
			const updated = await onUpdateProject({
				public_id: project.id,
				metadata: buildProjectMetadataWithSkills(project, nextSkills),
			});
			if (updated) {
				toast.success("项目技能已更新");
			}
		} finally {
			setSavingSkills(false);
		}
	};

	const addProjectSkill = (skill: ProjectSkill) => {
		if (savingSkills || selectedSkillCodes.includes(skill.code)) return;
		void updateProjectSkills([...project.skills, skill]);
	};

	const removeProjectSkill = (skillCode: string) => {
		if (savingSkills) return;
		void updateProjectSkills(project.skills.filter((skill) => skill.code !== skillCode));
	};

	return (
		<div className="no-scrollbar min-h-0 flex-1 space-y-7 overflow-y-auto px-5 py-6 pr-4">
			<div className="flex items-start justify-between gap-3">
				<div>
					<p className="text-sm font-semibold text-[var(--leros-text-strong)]">项目配置</p>
					<p className="mt-1 text-xs text-[var(--leros-text-muted)]">管理项目描述和可用技能</p>
				</div>
				<button
					type="button"
					className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-text-strong)]"
					aria-label="收起右侧栏"
					title="收起右侧栏"
					onClick={onCollapse}
				>
					<ChevronsRight className="size-4" />
				</button>
			</div>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">项目描述</h2>
					{!editingDescription && (
						<button
							type="button"
							className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
							aria-label="编辑项目描述"
							onClick={() => setEditingDescription(true)}
						>
							<Pencil className="size-3.5" />
						</button>
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
						<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">AI队友</h2>
						<span className="text-xs text-[var(--leros-text-subtle)]">0</span>
					</div>
					<button
						type="button"
						className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
						aria-label="添加 AI 队友"
						onClick={notifyFeatureUnavailable}
					>
						<Plus className="size-4" />
					</button>
				</div>
				<div className="max-h-36 overflow-y-auto rounded-xl border border-[var(--leros-control-border)] bg-white p-4">
					<p className="px-3 py-4 text-center text-xs text-[var(--leros-text-subtle)]">
						暂无 AI 队友
					</p>
				</div>
			</section>

			<section>
				<div className="mb-3 flex items-center justify-between gap-3">
					<div className="flex items-center gap-2">
						<h2 className="text-sm font-semibold text-[var(--leros-text-strong)]">技能</h2>
						<span className="text-xs text-[var(--leros-text-subtle)]">{project.skills.length}</span>
					</div>
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
							collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
							className="w-[340px] p-1.5"
						>
							<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
								<div className="px-2 py-1 text-xs font-medium text-slate-400">选择技能</div>
								<CommandInput
									value={skillSearch}
									onValueChange={setSkillSearch}
									placeholder="搜索技能"
								/>
								<CommandList className="max-h-64">
									<CommandEmpty className="py-6 text-slate-400">没有可继续添加的技能</CommandEmpty>
									<CommandGroup className="p-0">
										{skillsLoading && (
											<div className="px-3 py-2 text-xs text-slate-400">技能加载中...</div>
										)}
										{!skillsLoading && skillsError && (
											<div className="px-3 py-2 text-xs text-red-400">{skillsError}</div>
										)}
										{filteredSkills.map((skill) => (
											<CommandItem
												key={skill.code}
												value={skill.name}
												onSelect={() => addProjectSkill(skill)}
												className="rounded-xl px-2.5 py-2"
											>
												<SkillPickerIcon />
												<div className="min-w-0 flex-1">
													<div className="truncate font-medium">/{skill.name}</div>
													<div className="truncate text-xs text-slate-400">
														{skill.description || "项目可用技能"}
													</div>
												</div>
												<Check className="size-4 opacity-0" />
											</CommandItem>
										))}
									</CommandGroup>
								</CommandList>
							</Command>
						</PopoverContent>
					</Popover>
				</div>
				<div className="max-h-[280px] overflow-y-auto rounded-xl border border-[var(--leros-control-border)] bg-white p-4">
					{project.skills.length === 0 ? (
						<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-3 py-4 text-center text-xs text-[var(--leros-text-subtle)]">
							暂无技能
						</div>
					) : (
						<div className="flex flex-wrap gap-2">
							{project.skills.map((skill) => (
								<div
									key={skill.code}
									className="group relative inline-flex items-center gap-2 rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] py-1.5 pl-1.5 pr-2"
								>
									<SkillPickerIcon />
									<span className="max-w-[140px] truncate text-xs font-medium text-[var(--leros-text)]">
										{skill.name}
									</span>
									<button
										type="button"
										className="rounded-full p-0.5 text-[var(--leros-text-subtle)] opacity-0 transition-opacity hover:bg-[var(--leros-control-border)] hover:text-[var(--leros-text)] group-hover:opacity-100"
										aria-label={`移除技能 ${skill.name}`}
										onClick={() => removeProjectSkill(skill.code)}
										disabled={savingSkills}
									>
										<X className="size-3" />
									</button>
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

function installedSkillToProjectSkill(skill: SkillInstalledItem): ProjectSkill {
	return {
		code: skill.name,
		name: skill.display_name || skill.name,
		description: skill.description,
		category: skill.category,
		source: skill.source,
		trust: skill.trust,
	};
}

function buildProjectMetadataWithSkills(
	project: Project,
	skills: ProjectSkill[],
): Record<string, unknown> {
	const metadata = project.metadata ?? {};
	const extra = isPlainRecord(metadata.extra) ? metadata.extra : {};

	return {
		...metadata,
		extra: {
			...extra,
			skills: skills.map((skill) => ({
				code: skill.code,
				name: skill.name,
				description: skill.description,
				category: skill.category,
				source: skill.source,
				trust: skill.trust,
			})),
		},
	};
}

function normalizeInstalledSkillsPayload(value: unknown): SkillInstalledItem[] {
	const toItems = (items: unknown[]) =>
		items.map(skillItemFromValue).filter((item): item is SkillInstalledItem => item !== null);

	if (Array.isArray(value)) return toItems(value);
	if (!isPlainRecord(value)) return [];

	const nestedData = value.data;
	if (isPlainRecord(nestedData)) {
		if (Array.isArray(nestedData.skills)) return toItems(nestedData.skills);
		if (Array.isArray(nestedData.items)) return toItems(nestedData.items);
	}

	if (Array.isArray(value.skills)) return toItems(value.skills);
	if (Array.isArray(value.items)) return toItems(value.items);
	return [];
}

function skillItemFromValue(value: unknown): SkillInstalledItem | null {
	if (!isPlainRecord(value)) return null;

	const name = stringFromValue(value.name || value.skill_id || value.id);
	if (!name) return null;

	return {
		name,
		display_name: stringFromValue(value.display_name),
		description: stringFromValue(value.description),
		category: stringFromValue(value.category),
		source: stringFromValue(value.source || value.source_type),
		trust: stringFromValue(value.trust),
	};
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringFromValue(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function ProjectEmptyState({ layout }: { layout: ReturnType<typeof getProjectChatLayoutClasses> }) {
	return (
		<div className={cn("flex h-full", layout.shell)}>
			<div className={cn(layout.inner, "flex h-full items-center justify-center")}>
				<div className="flex max-w-[320px] flex-col items-center text-center">
					<div className="flex size-12 items-center justify-center rounded-full bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
						<Bot className="size-6" />
					</div>
					<h2 className="mt-5 text-lg font-semibold text-[var(--leros-text-strong)]">
						开始项目会话
					</h2>
					<p className="mt-2 text-sm leading-6 text-[var(--leros-text-muted)]">
						把需求、问题或上下文发给 AI，后续讨论会沉淀在当前项目中。
					</p>
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
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">任务</h2>
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
							{!compact && (
								<div className="pointer-events-none absolute right-4 top-4 flex items-center gap-1 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100">
									<button
										type="button"
										className="rounded p-0.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
										onClick={(event) => {
											event.stopPropagation();
											onRename?.(task);
										}}
										title="重命名任务"
									>
										<Pencil className="size-4" />
									</button>
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
								</div>
							)}
						</div>
					);
				})}
			</div>
		</div>
	);
}

function ProjectFiles({
	projectId,
	files,
}: {
	projectId: string;
	files: ProjectFileNode[];
	onRefresh: () => Promise<void>;
}) {
	const [previewFile, setPreviewFile] = useState<ProjectFileNode | null>(null);
	const [previewState, setPreviewState] = useState<FilePreviewState>({
		status: "idle",
	});
	const [uploading] = useState(false);
	const [uploadError] = useState<string | null>(null);
	const [searchKeyword, setSearchKeyword] = useState("");
	const [fileSourceFilter, setFileSourceFilter] = useState<"all" | FileSource>("all");
	const [drawerWidth, setDrawerWidth] = useState(FILE_PREVIEW_DRAWER_DEFAULT_WIDTH);
	const drawerRef = useRef<HTMLDivElement>(null);

	const closePreview = () => {
		setPreviewFile(null);
		setPreviewState({ status: "idle" });
	};

	const allFlatFiles = useMemo(() => {
		const allFiles = collectSelectableFiles(files);
		const keyword = searchKeyword.trim().toLowerCase();
		let filtered = allFiles;
		if (fileSourceFilter !== "all") {
			filtered = filtered.filter((f) => getFileSource(f.path) === fileSourceFilter);
		}
		if (keyword) {
			filtered = filtered.filter((file) => file.name.toLowerCase().includes(keyword));
		}
		return sortProjectFilesByUploadedTimeDesc(filtered);
	}, [files, searchKeyword, fileSourceFilter]);

	useEffect(() => {
		if (!previewFile) {
			setPreviewState({ status: "idle" });
			return;
		}
		const currentFile = previewFile;

		let cancelled = false;
		let objectUrl: string | null = null;
		const controller = new AbortController();

		async function loadPreview() {
			setPreviewState({ status: "loading" });
			try {
				const response = currentFile.storageUri
					? await fetchFilePreviewByStorageUri(currentFile.storageUri, {
							signal: controller.signal,
						})
					: await projectFileApi.fetchDownload(projectId, currentFile.path, {
							signal: controller.signal,
						});
				const mimeType =
					response.headers.get("content-type") ??
					currentFile.mimeType ??
					"application/octet-stream";

				const officeFormat = getOfficeOpenXmlFormat(currentFile.path, mimeType);
				if (officeFormat) {
					const buffer = await response.arrayBuffer();
					if (!cancelled) {
						setPreviewState({ status: "office", format: officeFormat, buffer });
					}
					return;
				}

				if (isSpreadsheetPreviewable(currentFile.path, mimeType)) {
					const buffer = await response.arrayBuffer();
					if (!cancelled) {
						setPreviewState({ status: "spreadsheet", buffer });
					}
					return;
				}

				if (isTextPreviewable(currentFile.path, mimeType)) {
					const content = await response.text();
					if (!cancelled) {
						setPreviewState({
							status: isMarkdownPreviewable(currentFile.path, mimeType) ? "markdown" : "text",
							content,
						});
					}
					return;
				}

				const blob = await response.blob();
				objectUrl = URL.createObjectURL(blob);
				if (!cancelled) {
					setPreviewState({ status: "blob", url: objectUrl, mimeType });
				}
			} catch (err) {
				if (cancelled || controller.signal.aborted) return;
				setPreviewState({
					status: "error",
					message: err instanceof Error ? err.message : "文件预览加载失败",
				});
			}
		}

		loadPreview();
		return () => {
			cancelled = true;
			controller.abort();
			if (objectUrl) {
				URL.revokeObjectURL(objectUrl);
			}
		};
	}, [previewFile, projectId]);

	useEffect(() => {
		if (!previewFile) return;

		const handlePointerDown = (event: PointerEvent) => {
			const target = event.target;
			if (!(target instanceof Element)) return;
			if (drawerRef.current?.contains(target)) return;
			if (target.closest("[data-file-preview-trigger]")) return;
			closePreview();
		};

		document.addEventListener("pointerdown", handlePointerDown);
		return () => document.removeEventListener("pointerdown", handlePointerDown);
	}, [previewFile]);

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
			const response = file.storageUri
				? await fetchFilePreviewByStorageUri(file.storageUri)
				: await projectFileApi.fetchDownload(projectId, file.path);
			const blob = await response.blob();
			const objectUrl = URL.createObjectURL(blob);
			const link = document.createElement("a");
			link.href = objectUrl;
			link.download = file.name;
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
		} catch (err) {
			console.error("ProjectFiles download error:", err);
		}
	};

	const handleDrawerResizeStart = (event: React.PointerEvent<HTMLElement>) => {
		event.preventDefault();
		const startX = event.clientX;
		const startWidth = drawerWidth;

		const handlePointerMove = (moveEvent: PointerEvent) => {
			const candidateWidth = startWidth - (moveEvent.clientX - startX);
			const maxWidth = Math.min(FILE_PREVIEW_DRAWER_MAX_WIDTH, window.innerWidth - 160);
			const nextWidth = Math.min(
				Math.max(candidateWidth, FILE_PREVIEW_DRAWER_MIN_WIDTH),
				Math.max(FILE_PREVIEW_DRAWER_MIN_WIDTH, maxWidth),
			);
			setDrawerWidth(nextWidth);
		};

		const handlePointerUp = () => {
			window.removeEventListener("pointermove", handlePointerMove);
			window.removeEventListener("pointerup", handlePointerUp);
		};

		window.addEventListener("pointermove", handlePointerMove);
		window.addEventListener("pointerup", handlePointerUp);
	};

	return (
		<div className="h-full overflow-y-auto px-10 py-7">
			<div className="mx-auto w-full max-w-[1200px]">
				<div className="mb-7 flex items-center justify-between gap-5">
					<div>
						<h2 className="text-[2rem] font-semibold tracking-tight text-[var(--leros-text-strong)]">
							项目文件
						</h2>
						<p className="mt-0.5 text-[13px] text-[var(--leros-text-muted)]">
							管理当前项目的所有文件资源
						</p>
					</div>
					{/* 中文注释：项目文件页顶部筛选条整体收一档，保持结构不变，只降低高度和横向占比，让桌面端视觉更紧凑。 */}
					<div className="flex items-center gap-2.5">
						<div className="relative">
							<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
							<input
								value={searchKeyword}
								onChange={(event) => setSearchKeyword(event.target.value)}
								placeholder="搜索文件..."
								className="h-9 w-60 rounded-xl border border-[var(--leros-control-border)] bg-white pl-9 pr-3.5 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
							/>
						</div>
						<div className="relative">
							<select
								value={fileSourceFilter}
								onChange={(event) => setFileSourceFilter(event.target.value as "all" | FileSource)}
								className="h-9 min-w-[132px] cursor-pointer appearance-none rounded-xl border border-[var(--leros-control-border)] bg-white py-0 pl-3.5 pr-9 text-[13px] outline-none transition-colors focus:border-[var(--leros-primary)]"
							>
								<option value="all">全部</option>
								<option value="task">任务文件</option>
								<option value="upload">上传文件</option>
							</select>
							<ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
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

				{allFlatFiles.length === 0 ? (
					<div className="px-6 py-16 text-center text-sm text-[var(--leros-text-muted)]">
						暂无文件
					</div>
				) : (
					<div className="overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-white">
						<div className="grid grid-cols-[minmax(0,1fr)_90px_120px_180px_180px] border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-3.5 text-[11px] font-semibold uppercase tracking-wider text-[var(--leros-text-muted)]">
							<div>名称</div>
							<div>类型</div>
							<div>大小</div>
							<div>创建时间</div>
							<div className="text-right">操作</div>
						</div>
						<div className="divide-y divide-[var(--leros-control-border)]/60">
							{allFlatFiles.map((file) => (
								<div
									key={file.path}
									className="grid grid-cols-[minmax(0,1fr)_90px_120px_180px_180px] items-center px-5 py-4 transition-colors hover:bg-[var(--leros-primary-softer)]/25"
								>
									<button
										type="button"
										data-file-preview-trigger
										onClick={() => setPreviewFile(file)}
										className="flex min-w-0 cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1 text-left transition-colors hover:bg-[var(--leros-primary-softer)]/50"
										title="查看"
									>
										<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
											<ProjectFileTypeIcon fileName={file.name} />
										</div>
										<div className="min-w-0">
											<p className="truncate text-[15px] font-semibold text-[var(--leros-text-strong)]">
												{file.name}
											</p>
										</div>
									</button>
									<div className="text-[13px]">
										<span className="inline-block rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-0.5 text-[11px] font-medium text-[var(--leros-text-muted)]">
											{getFileSource(file.path) === "task" ? "任务文件" : "上传文件"}
										</span>
									</div>
									<div className="text-[13px] text-[var(--leros-text-muted)]">
										{formatBytes(file.size)}
									</div>
									<div className="text-[13px] text-[var(--leros-text-muted)]">
										{formatTime(file.createdAt)}
									</div>
									<div className="flex items-center justify-end gap-1.5">
										<button
											type="button"
											onClick={() => setPreviewFile(file)}
											className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[13px] text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
											title="查看"
										>
											<Eye className="size-4" />
											查看
										</button>
										<button
											type="button"
											onClick={() => handleDownload(file)}
											className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[13px] text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
											title="下载"
										>
											<Download className="size-4" />
											下载
										</button>
									</div>
								</div>
							))}
						</div>
					</div>
				)}
			</div>

			{previewFile && (
				<div
					ref={drawerRef}
					className="fixed right-0 top-16 z-40 flex h-[calc(100vh-64px)] flex-col overflow-hidden border-l border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-0 shadow-2xl rounded-l-2xl"
					style={{ width: `${drawerWidth}px`, maxWidth: `${drawerWidth}px` }}
				>
					<button
						type="button"
						aria-label="拖动调整预览宽度"
						title="拖动调整预览宽度"
						onPointerDown={handleDrawerResizeStart}
						className="absolute left-0 top-0 z-10 flex h-full w-4 -translate-x-1/2 cursor-col-resize items-center justify-center"
					>
						<div className="flex h-16 w-2 items-center justify-center rounded-full bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] shadow-sm ring-1 ring-[var(--leros-control-border)]">
							<ChevronsLeftRightEllipsis className="size-3" />
						</div>
					</button>
					<div className="flex items-center justify-between border-b border-[var(--leros-control-border)] px-6 py-4">
						<div className="min-w-0">
							<div className="truncate text-lg font-medium text-[var(--leros-text-strong)]">
								{previewFile.name}
							</div>
							<div className="mt-1 truncate text-xs text-[var(--leros-text-muted)]">
								/{previewFile.path}
							</div>
						</div>
						<div className="flex items-center gap-2">
							<button
								type="button"
								onClick={() => handleDownload(previewFile)}
								className="rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)]"
								title="下载"
							>
								<Download className="size-4" />
							</button>
							<button
								type="button"
								onClick={closePreview}
								className="rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)]"
								title="关闭"
							>
								<X className="size-4" />
							</button>
						</div>
					</div>
					<div className="min-h-0 flex-1 overflow-auto bg-[var(--leros-surface-soft)] p-6">
						<ProjectFilePreviewBody file={previewFile} previewState={previewState} />
					</div>
				</div>
			)}
		</div>
	);
}

function ProjectFilePreviewBody({
	file,
	previewState,
}: {
	file: ProjectFileNode;
	previewState: FilePreviewState;
}) {
	if (previewState.status === "idle" || previewState.status === "loading") {
		return (
			<div className="flex h-full min-h-[320px] items-center justify-center text-sm text-[var(--leros-text-muted)]">
				<LoaderCircle className="mr-2 size-4 animate-spin" />
				加载预览中
			</div>
		);
	}

	if (previewState.status === "error") {
		return (
			<div className="flex h-full min-h-[320px] items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
				<div>
					<p>无法加载文件预览</p>
					<p className="mt-1 text-xs">{previewState.message}</p>
				</div>
			</div>
		);
	}

	if (previewState.status === "text") {
		return (
			<pre className="overflow-auto rounded-xl bg-white p-4 text-sm leading-6 text-[var(--leros-text)] shadow-sm">
				{previewState.content}
			</pre>
		);
	}

	if (previewState.status === "markdown") {
		return (
			<div className="overflow-auto rounded-xl bg-white px-8 py-7 shadow-sm">
				<MarkdownRenderer
					content={previewState.content}
					className="prose prose-slate prose-sm max-w-none prose-headings:text-[var(--leros-text-strong)] prose-p:leading-7 prose-pre:rounded-lg prose-pre:bg-slate-950"
				/>
			</div>
		);
	}

	if (previewState.status === "office") {
		return (
			<div className="h-[calc(100vh-150px)] min-h-[520px] overflow-hidden rounded-xl bg-white shadow-sm">
				<OfficePreview
					fileName={file.name}
					buffer={previewState.buffer}
					format={previewState.format}
				/>
			</div>
		);
	}

	if (previewState.status === "spreadsheet") {
		return (
			<div className="h-[calc(100vh-150px)] min-h-[520px] overflow-hidden rounded-xl bg-white shadow-sm">
				<SpreadsheetPreview buffer={previewState.buffer} fileName={file.name} />
			</div>
		);
	}

	if (previewState.mimeType.startsWith("image/")) {
		return (
			<div className="flex min-h-[320px] items-center justify-center rounded-xl bg-white p-4 shadow-sm">
				<img
					src={previewState.url}
					alt={file.name}
					className="max-h-full max-w-full object-contain"
				/>
			</div>
		);
	}

	if (previewState.mimeType.includes("pdf")) {
		return (
			<div className="overflow-hidden rounded-xl bg-white shadow-sm">
				<iframe
					title={file.name}
					src={previewState.url}
					className="h-[calc(100vh-150px)] min-h-[760px] w-full border-0 bg-white"
				/>
			</div>
		);
	}

	return (
		<div className="flex min-h-[320px] items-center justify-center rounded-xl bg-white px-8 text-center text-sm text-[var(--leros-text-muted)] shadow-sm">
			<div>
				<FileText className="mx-auto mb-3 size-8 text-[var(--leros-text-subtle)]" />
				<p>此文件类型暂不支持内嵌预览</p>
				<p className="mt-1 text-xs">请使用下载按钮在本地查看</p>
			</div>
		</div>
	);
}

function isTextPreviewable(path: string, mimeType: string): boolean {
	const normalizedPath = path.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	if (normalizedMimeType.startsWith("text/")) return true;
	if (normalizedMimeType.includes("json")) return true;
	if (normalizedMimeType.includes("javascript")) return true;
	if (normalizedMimeType.includes("typescript")) return true;

	return [
		".md",
		".markdown",
		".txt",
		".json",
		".js",
		".jsx",
		".ts",
		".tsx",
		".css",
		".html",
		".xml",
		".yml",
		".yaml",
		".go",
		".py",
		".java",
		".sh",
		".sql",
	].some((suffix) => normalizedPath.endsWith(suffix));
}

function isMarkdownPreviewable(path: string, mimeType: string): boolean {
	const normalizedPath = path.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	return (
		normalizedMimeType.includes("markdown") ||
		normalizedPath.endsWith(".md") ||
		normalizedPath.endsWith(".markdown")
	);
}

function isSpreadsheetPreviewable(path: string, mimeType: string): boolean {
	const normalizedPath = path.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	return (
		normalizedMimeType.includes("spreadsheet") ||
		normalizedMimeType.includes("excel") ||
		normalizedMimeType === "text/csv" ||
		[".xls", ".csv"].some((suffix) => normalizedPath.endsWith(suffix))
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
	return new Intl.DateTimeFormat("zh-CN", {
		year: "numeric",
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
	}).format(new Date(timestamp * 1000));
}

function clampProjectRightSidebarWidth(width: number): number {
	return Math.min(
		PROJECT_RIGHT_SIDEBAR_MAX_WIDTH,
		Math.max(PROJECT_RIGHT_SIDEBAR_MIN_WIDTH, Math.round(width)),
	);
}
