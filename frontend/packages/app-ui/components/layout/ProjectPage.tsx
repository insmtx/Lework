"use client";

import type { ProjectTask } from "@leros/store";
import { projectFileApi, useChatStore, useLayoutStore } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	ChevronDown,
	ChevronsLeft,
	ChevronsLeftRightEllipsis,
	ChevronsRight,
	Download,
	Eye,
	FileText,
	LayoutPanelLeft,
	LoaderCircle,
	Search,
	Settings,
	Trash2,
	X,
} from "lucide-react";
import {
	type ComponentType,
	type CSSProperties,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { MessageTimeline } from "../chat/MessageTimeline";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { ChatInput } from "../input/ChatInput";
import { ArtifactPreviewDialog, type ArtifactPreviewItem } from "./ArtifactPreviewDialog";
import type { AppNavigation } from "./LeftRail";
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
const PROJECT_RIGHT_SIDEBAR_WIDTH_STORAGE_KEY = "leros-project-right-sidebar-width";
const PROJECT_RIGHT_SIDEBAR_COLLAPSED_STORAGE_KEY = "leros-project-right-sidebar-collapsed";
const PROJECT_RIGHT_SIDEBAR_DEFAULT_WIDTH = 300;
const PROJECT_RIGHT_SIDEBAR_MIN_WIDTH = 260;
const PROJECT_RIGHT_SIDEBAR_MAX_WIDTH = 420;
const PROJECT_RIGHT_SIDEBAR_WIDE_BREAKPOINT = 360;

type ProjectTab = (typeof projectTabs)[number]["id"];

type FilePreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "error"; message: string }
	| { status: "docx"; buffer: ArrayBuffer }
	| { status: "markdown"; content: string }
	| { status: "text"; content: string }
	| { status: "spreadsheet"; buffer: ArrayBuffer }
	| { status: "blob"; url: string; mimeType: string };

type DocxEditorComponent = ComponentType<{
	documentBuffer?: ArrayBuffer | null;
	mode?: "editing" | "suggesting" | "viewing";
	readOnly?: boolean;
	showToolbar?: boolean;
	showZoomControl?: boolean;
	showRuler?: boolean;
	showOutline?: boolean;
	showOutlineButton?: boolean;
	disableFindReplaceShortcuts?: boolean;
	initialZoom?: number;
	className?: string;
	style?: CSSProperties;
	documentName?: string;
	documentNameEditable?: boolean;
	loadingIndicator?: React.ReactNode;
	onError?: (error: Error) => void;
}>;

let docxEditorComponent: DocxEditorComponent | null = null;
let docxEditorPromise: Promise<DocxEditorComponent> | null = null;

function loadDocxEditor(): Promise<DocxEditorComponent> {
	if (docxEditorComponent) return Promise.resolve(docxEditorComponent);
	docxEditorPromise ??= import("@eigenpal/docx-editor-react").then((module) => {
		docxEditorComponent = module.DocxEditor as DocxEditorComponent;
		return docxEditorComponent;
	});
	return docxEditorPromise;
}

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

	const flatArtifactFiles = useMemo(
		() =>
			sortProjectFilesByUploadedTimeDesc(
				collectSelectableFiles(projectFiles).filter((f) => getFileSource(f.path) === "task"),
			),
		[projectFiles],
	);

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
			<header className="flex h-16 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				<div className="flex items-center gap-3 text-[var(--leros-text-muted)]">
					<h1 className="text-base font-bold text-[var(--leros-text-strong)]">{project.name}</h1>
				</div>
				<div className="flex items-center gap-6 text-[var(--leros-text)]">
					<button
						type="button"
						className="rounded-full p-1.5 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<Search className="size-5" />
					</button>
					{showProjectSidebar && (
						<button
							type="button"
							className="rounded-full p-1.5 transition-colors hover:bg-[var(--leros-primary-softer)]"
							aria-label={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
							title={rightSidebarCollapsed ? "展开右侧栏" : "收起右侧栏"}
							onClick={() => setRightSidebarCollapsed((collapsed) => !collapsed)}
						>
							<LayoutPanelLeft className="size-5" />
						</button>
					)}
					<button
						type="button"
						className="rounded-full p-1.5 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<Settings className="size-5" />
					</button>
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
						<div className="no-scrollbar min-h-0 flex-1 space-y-8 overflow-y-auto px-5 py-6 pr-4">
							<div className="flex items-start justify-between gap-3">
								<div>
									<p className="text-sm font-semibold text-[var(--leros-text-strong)]">项目侧栏</p>
									<p className="mt-1 text-xs text-[var(--leros-text-muted)]">查看任务和文件概览</p>
								</div>
								<button
									type="button"
									className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-text-strong)]"
									aria-label="收起右侧栏"
									title="收起右侧栏"
									onClick={() => setRightSidebarCollapsed(true)}
								>
									<ChevronsRight className="size-4" />
								</button>
							</div>
							<section>
								<div
									className={cn(
										"mb-4 flex w-full items-center justify-between",
										!isWideRightSidebar && "mx-auto max-w-[250px]",
									)}
								>
									<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">任务</h2>
									<span className="rounded-md bg-[var(--leros-primary-soft)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
										{project.tasks.length} 项
									</span>
								</div>
								<ProjectTaskList
									tasks={project.tasks}
									compact={!isWideRightSidebar}
									onOpen={handleOpenTask}
								/>
							</section>

							<section>
								<div
									className={cn(
										"mb-4 flex w-full items-center justify-between",
										!isWideRightSidebar && "mx-auto max-w-[250px]",
									)}
								>
									<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">文件</h2>
									<span className="rounded-md bg-[var(--leros-primary-soft)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
										{flatArtifactFiles.length} 个
									</span>
								</div>
								<ProjectFileList
									files={flatArtifactFiles}
									compact={!isWideRightSidebar}
									projectId={resolvedProjectId || ""}
								/>
							</section>
						</div>
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
	const [deleteTarget, setDeleteTarget] = useState<ProjectTask | null>(null);

	return (
		// 中文注释：任务 tab 需要占用更宽的主内容区域，避免大屏下卡片挤在中间留下过多留白。
		<div className="mx-auto w-full max-w-[1100px]">
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">任务</h2>
			<div className="mt-4">
				<ProjectTaskList tasks={tasks} onDelete={setDeleteTarget} onOpen={onOpenTask} />
			</div>
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
	onDelete,
	onOpen,
}: {
	tasks: ProjectTask[];
	compact?: boolean;
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
								className={cn(contentClassName, "pr-11")}
								onClick={() => onOpen?.(task)}
								disabled={!onOpen}
								title={onOpen ? "打开任务会话" : undefined}
							>
								{content}
							</button>
							{!compact && (
								<button
									type="button"
									className="pointer-events-none absolute right-4 top-4 rounded p-0.5 text-[var(--leros-text-muted)] opacity-0 transition-opacity hover:bg-[var(--leros-danger-softer)] hover:text-[var(--leros-danger)] group-hover:pointer-events-auto group-hover:opacity-100"
									onClick={(event) => {
										event.stopPropagation();
										onDelete(task);
									}}
									title="删除任务"
								>
									<Trash2 className="size-4" />
								</button>
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
				const response = await projectFileApi.fetchDownload(projectId, currentFile.path, {
					signal: controller.signal,
				});
				const mimeType =
					response.headers.get("content-type") ??
					currentFile.mimeType ??
					"application/octet-stream";

				if (isDocxPreviewable(currentFile.path, mimeType)) {
					const buffer = await response.arrayBuffer();
					if (!cancelled) {
						setPreviewState({ status: "docx", buffer });
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
			const response = await projectFileApi.fetchDownload(projectId, file.path);
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
		<div className="h-full overflow-y-auto px-10 py-8">
			<div className="mx-auto w-full max-w-[1200px]">
				<div className="mb-8 flex items-center justify-between gap-6">
					<div>
						<h2 className="text-2xl font-semibold tracking-tight text-[var(--leros-text-strong)]">
							项目文件
						</h2>
						<p className="mt-1 text-sm text-[var(--leros-text-muted)]">
							管理当前项目的所有文件资源
						</p>
					</div>
					<div className="flex items-center gap-3">
						<div className="relative">
							<Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--leros-text-muted)]" />
							<input
								value={searchKeyword}
								onChange={(event) => setSearchKeyword(event.target.value)}
								placeholder="搜索文件..."
								className="h-10 w-64 rounded-xl border border-[var(--leros-control-border)] bg-white pl-9 pr-4 text-sm outline-none transition-colors focus:border-[var(--leros-primary)]"
							/>
						</div>
						<div className="relative">
							<select
								value={fileSourceFilter}
								onChange={(event) => setFileSourceFilter(event.target.value as "all" | FileSource)}
								className="h-10 cursor-pointer appearance-none rounded-xl border border-[var(--leros-control-border)] bg-white py-0 pl-3.5 pr-9 text-sm outline-none transition-colors focus:border-[var(--leros-primary)]"
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
						<div className="grid grid-cols-[minmax(0,1fr)_90px_120px_180px_180px] border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-6 py-4 text-xs font-semibold uppercase tracking-wider text-[var(--leros-text-muted)]">
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
									className="grid grid-cols-[minmax(0,1fr)_90px_120px_180px_180px] items-center px-6 py-5 transition-colors hover:bg-[var(--leros-primary-softer)]/25"
								>
									<button
										type="button"
										data-file-preview-trigger
										onClick={() => setPreviewFile(file)}
										className="flex min-w-0 cursor-pointer items-center gap-3 rounded-lg px-2 py-1 text-left transition-colors hover:bg-[var(--leros-primary-softer)]/50"
										title="查看"
									>
										<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
											<ProjectFileTypeIcon fileName={file.name} />
										</div>
										<div className="min-w-0">
											<p className="truncate text-sm font-semibold text-[var(--leros-text-strong)]">
												{file.name}
											</p>
											<p className="truncate text-xs text-[var(--leros-text-muted)]">
												/{file.path}
											</p>
										</div>
									</button>
									<div className="text-sm">
										<span className="inline-block rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]">
											{getFileSource(file.path) === "task" ? "任务文件" : "上传文件"}
										</span>
									</div>
									<div className="text-sm text-[var(--leros-text-muted)]">
										{formatBytes(file.size)}
									</div>
									<div className="text-sm text-[var(--leros-text-muted)]">
										{formatTime(file.createdAt)}
									</div>
									<div className="flex items-center justify-end gap-2">
										<button
											type="button"
											onClick={() => setPreviewFile(file)}
											className="inline-flex items-center gap-1 rounded-lg px-3 py-2 text-sm text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
											title="查看"
										>
											<Eye className="size-4" />
											查看
										</button>
										<button
											type="button"
											onClick={() => handleDownload(file)}
											className="inline-flex items-center gap-1 rounded-lg px-3 py-2 text-sm text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
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

	if (previewState.status === "docx") {
		return (
			<div className="h-[calc(100vh-150px)] min-h-[520px] overflow-hidden rounded-xl bg-white shadow-sm">
				<ProjectDocxPreview
					documentName={file.name}
					documentKey={file.path}
					buffer={previewState.buffer}
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

function ProjectDocxPreview({
	documentName,
	documentKey,
	buffer,
}: {
	documentName: string;
	documentKey: string;
	buffer: ArrayBuffer;
}) {
	const [DocxEditor, setDocxEditor] = useState<DocxEditorComponent | null>(docxEditorComponent);
	const [error, setError] = useState<string | null>(null);

	useEffect(() => {
		let cancelled = false;
		setError(null);
		// 这里复用和文件预览一致的懒加载模式，保证文件 tab 的 DOCX 体验对齐。
		loadDocxEditor()
			.then((component) => {
				if (!cancelled) setDocxEditor(() => component);
			})
			.catch((err) => {
				if (cancelled) return;
				setError(err instanceof Error ? err.message : "DOCX 预览组件加载失败");
			});
		return () => {
			cancelled = true;
		};
	}, []);

	if (error) {
		return (
			<div className="flex h-full items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
				<div>
					<p>无法加载 DOCX 预览</p>
					<p className="mt-1 text-xs">{error}</p>
				</div>
			</div>
		);
	}

	if (!DocxEditor) {
		return <div className="h-full bg-white" />;
	}

	return (
		<div className="h-full overflow-hidden">
			<DocxEditor
				key={documentKey}
				documentBuffer={buffer}
				mode="viewing"
				readOnly
				showToolbar={false}
				showZoomControl={false}
				showRuler={false}
				showOutline={false}
				showOutlineButton={false}
				disableFindReplaceShortcuts
				initialZoom={0.82}
				documentName={documentName}
				documentNameEditable={false}
				className="leros-docx-preview h-full"
				style={{ height: "100%", background: "#f6f7fb" }}
				loadingIndicator={<div className="h-full bg-[#f6f7fb]" />}
				onError={(err) => setError(err.message)}
			/>
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

function isDocxPreviewable(path: string, mimeType: string): boolean {
	const normalizedPath = path.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	return (
		normalizedMimeType ===
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		normalizedPath.endsWith(".docx")
	);
}

function isSpreadsheetPreviewable(path: string, mimeType: string): boolean {
	const normalizedPath = path.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	return (
		normalizedMimeType.includes("spreadsheet") ||
		normalizedMimeType.includes("excel") ||
		normalizedMimeType === "text/csv" ||
		[".xlsx", ".xls", ".csv"].some((suffix) => normalizedPath.endsWith(suffix))
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

function ProjectFileList({
	files,
	emptyText = "暂无文件",
	compact = false,
	projectId,
}: {
	files: ProjectFileNode[];
	emptyText?: string;
	compact?: boolean;
	projectId: string;
}) {
	const [previewArtifact, setPreviewArtifact] = useState<ArtifactPreviewItem | null>(null);

	if (files.length === 0) {
		return (
			<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]">
				{emptyText}
			</div>
		);
	}

	return (
		<>
			<div className={cn("w-full", compact && "mx-auto max-w-[250px]")}>
				<div className={cn(compact ? SIDEBAR_COMPACT_LIST_CLASS : "space-y-3")}>
					{files.map((file) => (
						<button
							type="button"
							key={file.path}
							onClick={() =>
								setPreviewArtifact({
									id: file.path,
									name: file.name,
									title: file.name,
									type: "document",
									artifactType: "file",
									mimeType: file.mimeType,
									size: formatBytes(file.size),
									downloadUrl: "",
								})
							}
							className={cn(
								"group relative flex w-full cursor-pointer items-center overflow-hidden border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-left shadow-sm transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35",
								compact ? "gap-3 rounded-lg px-3.5 py-3" : "gap-3.5 rounded-lg px-4 py-3.5",
							)}
							title="预览文件"
						>
							{/* hover 时补一个轻量蒙层，明确提示当前整卡可点击预览 */}
							<div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover:opacity-100">
								<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
									点击预览
								</span>
							</div>
							<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)]">
								<ProjectFileTypeIcon fileName={file.name} />
							</div>
							<div className="min-w-0">
								<div className="truncate text-sm font-normal leading-5 text-[var(--leros-text-strong)]">
									{file.name}
								</div>
								<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
									{file.size > 0 ? formatBytes(file.size) : ""}
								</div>
							</div>
						</button>
					))}
				</div>
			</div>
			<ArtifactPreviewDialog
				artifact={previewArtifact}
				open={previewArtifact !== null}
				onOpenChange={(open) => {
					if (!open) setPreviewArtifact(null);
				}}
				projectId={projectId}
			/>
		</>
	);
}
