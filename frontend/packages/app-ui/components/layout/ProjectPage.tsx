"use client";

import type { ProjectArtifact, ProjectTask } from "@leros/store";
import {
	fetchFileDownload,
	mapBackendArtifactToProjectArtifact,
	projectFileApi,
	useChatStore,
	useLayoutStore,
} from "@leros/store";
import { artifactApi } from "@leros/store/api/artifactApi";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	Calendar,
	CheckCircle2,
	ChevronsLeftRightEllipsis,
	Circle,
	Download,
	Eye,
	FileImage,
	FileText,
	LayoutPanelLeft,
	LoaderCircle,
	Search,
	Settings,
	Table2,
	Tag,
	Trash2,
	X,
} from "lucide-react";
import { type ChangeEvent, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { MessageTimeline } from "../chat/MessageTimeline";
import { ChatInput, PROJECT_ATTACHMENT_ACCEPT } from "../input/ChatInput";
import { ArtifactPreviewDialog } from "./ArtifactPreviewDialog";
import type { AppNavigation } from "./LeftRail";
import {
	collectSelectableFiles,
	normalizeProjectFileTree,
	type ProjectFileNode,
	sortProjectFilesByUploadedTimeDesc,
} from "./project-files";
import { SpreadsheetPreview } from "./SpreadsheetPreview";
import { TaskDeleteDialog } from "./TaskDeleteDialog";

const projectTabs = [
	{ id: "chat" as const, label: "会话" },
	{ id: "tasks" as const, label: "任务" },
	{ id: "files" as const, label: "文件" },
];

const FILE_PREVIEW_DRAWER_DEFAULT_WIDTH = 860;
const FILE_PREVIEW_DRAWER_MIN_WIDTH = 720;
const FILE_PREVIEW_DRAWER_MAX_WIDTH = 1200;

type ProjectTab = (typeof projectTabs)[number]["id"];

type FilePreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "error"; message: string }
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
	} = useLayoutStore((s) => s);

	const {
		activeSessionId,
		isGenerating,
		pendingBootstrapSessionId,
		setActiveSession,
		loadConversationMessages,
		resetLocalMessages,
	} = useChatStore((s) => s);

	const [taskArtifacts, setTaskArtifacts] = useState<ProjectArtifact[]>([]);
	const [projectFiles, setProjectFiles] = useState<ProjectFileNode[]>([]);

	const resolvedProjectId = projectId ?? activeProjectId;
	const resolvedTab = tab ?? activeProjectTab;
	const project =
		projects.find((item) => item.id === resolvedProjectId) ??
		(resolvedProjectId ? undefined : projects[0]);

	const taskIds = useMemo(() => project?.tasks.map((task) => task.id).join("|") ?? "", [project]);

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

	useEffect(() => {
		if (!taskIds) {
			setTaskArtifacts([]);
			return;
		}

		let cancelled = false;

		async function fetchTaskArtifacts() {
			const ids = taskIds.split("|").filter(Boolean);
			try {
				const responses = await Promise.all(
					ids.map((taskId) => artifactApi.listTaskArtifacts(taskId)),
				);
				if (cancelled) return;

				const merged = new Map<string, ProjectArtifact>();
				for (const response of responses) {
					for (const artifact of response.data.data ?? []) {
						const item = mapBackendArtifactToProjectArtifact(artifact);
						merged.set(item.id, item);
					}
				}
				setTaskArtifacts([...merged.values()]);
			} catch (err) {
				if (cancelled) return;
				console.error("ProjectPage fetch task artifacts error:", err);
				setTaskArtifacts([]);
			}
		}

		fetchTaskArtifacts();
		return () => {
			cancelled = true;
		};
	}, [taskIds]);

	const refreshProjectFiles = async () => {
		if (!resolvedProjectId) return;
		const response = await projectFileApi.list({
			projectId: resolvedProjectId,
			depth: 3,
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
					depth: 3,
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
					<button
						type="button"
						className="rounded-full p-1.5 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<LayoutPanelLeft className="size-5" />
					</button>
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

			<div className="min-h-0 flex flex-1">
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
					{resolvedTab === "chat" && <ProjectChat />}
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

				{resolvedTab !== "files" && (
					<aside className="flex w-[300px] shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-6">
						<div className="min-h-0 flex-1 space-y-8 overflow-y-auto pr-1">
							<section>
								<div className="mx-auto mb-4 flex w-full max-w-[250px] items-center justify-between">
									<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">任务</h2>
									<span className="rounded-md bg-[var(--leros-primary-soft)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
										{project.tasks.length} 项
									</span>
								</div>
								<ProjectTaskList tasks={project.tasks} compact onOpen={handleOpenTask} />
							</section>

							<section>
								<div className="mx-auto mb-4 flex w-full max-w-[250px] items-center justify-between">
									<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">产物</h2>
									<span className="rounded-md bg-[var(--leros-chat-control-bg)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-text)]">
										{taskArtifacts.length} 个
									</span>
								</div>
								<ProjectArtifactList artifacts={taskArtifacts} compact />
							</section>
						</div>
					</aside>
				)}
			</div>
		</div>
	);
}

function ProjectChat() {
	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<MessageTimeline
				emptyState={<ProjectEmptyState />}
				contentClassName="max-w-[780px] px-8 py-8 sm:px-8 lg:px-8"
			/>
			<ChatInput variant="project" />
		</div>
	);
}

function ProjectEmptyState() {
	return (
		<div className="flex h-full items-center justify-center px-8">
			<div className="flex max-w-[320px] flex-col items-center text-center">
				<div className="flex size-12 items-center justify-center rounded-full bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
					<Bot className="size-6" />
				</div>
				<h2 className="mt-5 text-lg font-semibold text-[var(--leros-text-strong)]">开始项目会话</h2>
				<p className="mt-2 text-sm leading-6 text-[var(--leros-text-muted)]">
					把需求、问题或上下文发给 AI，后续讨论会沉淀在当前项目中。
				</p>
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
	const [deleteTarget, setDeleteTarget] = useState<ProjectTask | null>(null);

	const handleStatusToggle = async (task: ProjectTask) => {
		await updateTask({
			public_id: task.id,
			status: NEXT_STATUS[task.status] ?? "todo",
		});
	};

	return (
		<div className="mx-auto w-full max-w-[720px]">
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">任务</h2>
			<div className="mt-4">
				<ProjectTaskList
					tasks={tasks}
					onStatusToggle={handleStatusToggle}
					onDelete={setDeleteTarget}
					onOpen={onOpenTask}
				/>
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

const NEXT_STATUS: Record<string, string> = {
	todo: "in_progress",
	in_progress: "done",
	done: "todo",
};

const STATUS_LABEL: Record<string, string> = {
	todo: "待办",
	in_progress: "进行中",
	done: "已完成",
};

function ProjectTaskList({
	tasks,
	compact = false,
	onStatusToggle,
	onDelete,
	onOpen,
}: {
	tasks: ProjectTask[];
	compact?: boolean;
	onStatusToggle?: (task: ProjectTask) => void;
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
		<div className={cn("w-full", compact ? "mx-auto max-w-[250px] space-y-3" : "space-y-3")}>
			{tasks.map((task) => (
				<div
					key={task.id}
					className={cn(
						"group flex items-start border border-[var(--leros-control-border)] bg-[var(--leros-surface)] shadow-sm",
						onOpen &&
							"cursor-pointer transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35",
						compact ? "gap-3 rounded-lg px-3.5 py-3" : "gap-3.5 rounded-lg px-4 py-3.5",
					)}
				>
					<button
						type="button"
						className="mt-0.5 shrink-0 cursor-pointer"
						onClick={(event) => {
							event.stopPropagation();
							onStatusToggle?.(task);
						}}
						title={`切换状态（当前：${STATUS_LABEL[task.status] ?? task.status}）`}
					>
						{task.status === "done" ? (
							<CheckCircle2 className="size-4 text-[var(--leros-primary)]" />
						) : task.status === "in_progress" ? (
							<LoaderCircle className="size-4 text-[var(--leros-warning)]" />
						) : (
							<Circle className="size-4 text-[var(--leros-text-muted)]" />
						)}
					</button>
					<button
						type="button"
						className="min-w-0 flex-1 text-left"
						onClick={() => onOpen?.(task)}
						disabled={!onOpen}
						title={onOpen ? "打开任务会话" : undefined}
					>
						<div className="truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
							{task.title}
						</div>
						<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
							{task.meta}
						</div>
						{!compact && (task.taskType || task.deadline) && (
							<div className="mt-2 flex flex-wrap items-center gap-2">
								{task.taskType && (
									<span className="inline-flex items-center gap-1 rounded-md bg-[var(--leros-primary-softer)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--leros-primary)]">
										<Tag className="size-3" />
										{task.taskType}
									</span>
								)}
								{task.deadline && (
									<span className="inline-flex items-center gap-1 rounded-md bg-[var(--leros-chat-control-bg)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--leros-text-muted)]">
										<Calendar className="size-3" />
										{task.deadline}
									</span>
								)}
							</div>
						)}
					</button>
					{!compact && onDelete && (
						<button
							type="button"
							className="mt-0.5 shrink-0 rounded p-0.5 text-[var(--leros-text-muted)] opacity-0 transition-opacity hover:bg-[var(--leros-danger-softer)] hover:text-[var(--leros-danger)] group-hover:opacity-100"
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
			))}
		</div>
	);
}

function ProjectFiles({
	projectId,
	files,
	onRefresh,
}: {
	projectId: string;
	files: ProjectFileNode[];
	onRefresh: () => Promise<void>;
}) {
	const selectableFiles = useMemo(() => collectSelectableFiles(files), [files]);
	const [previewFile, setPreviewFile] = useState<ProjectFileNode | null>(null);
	const [previewState, setPreviewState] = useState<FilePreviewState>({
		status: "idle",
	});
	const [uploading, setUploading] = useState(false);
	const [uploadError, setUploadError] = useState<string | null>(null);
	const [searchKeyword, setSearchKeyword] = useState("");
	const [drawerWidth, setDrawerWidth] = useState(FILE_PREVIEW_DRAWER_DEFAULT_WIDTH);
	const drawerRef = useRef<HTMLDivElement>(null);

	const closePreview = () => {
		setPreviewFile(null);
		setPreviewState({ status: "idle" });
	};

	const filteredFiles = useMemo(() => {
		const keyword = searchKeyword.trim().toLowerCase();
		const matchedFiles = !keyword
			? selectableFiles
			: selectableFiles.filter((file) => file.name.toLowerCase().includes(keyword));
		return sortProjectFilesByUploadedTimeDesc(matchedFiles);
	}, [searchKeyword, selectableFiles]);

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
			if (!currentFile.publicId) {
				setPreviewState({ status: "error", message: "文件缺少 public_id，无法预览" });
				return;
			}

			setPreviewState({ status: "loading" });
			try {
				const response = await fetchFileDownload(currentFile.publicId, {
					signal: controller.signal,
				});
				const mimeType =
					response.headers.get("content-type") ??
					currentFile.mimeType ??
					"application/octet-stream";

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
						setPreviewState({ status: "text", content });
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
	}, [previewFile]);

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

	const handleUpload = async (event: ChangeEvent<HTMLInputElement>) => {
		const file = event.target.files?.[0];
		event.target.value = "";
		if (!file) return;

		setUploading(true);
		setUploadError(null);
		try {
			const response = await projectFileApi.upload({ projectId, file });
			await onRefresh();
			toast.success(response.message || "文件上传成功");
		} catch (err) {
			setUploadError(err instanceof Error ? err.message : "上传文件失败");
		} finally {
			setUploading(false);
		}
	};

	const handleDownload = async (file: ProjectFileNode) => {
		if (!file.publicId) {
			console.error("ProjectFiles download error: missing public_id");
			return;
		}

		try {
			const response = await fetchFileDownload(file.publicId);
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
						<label className="inline-flex cursor-pointer items-center gap-2 rounded-xl bg-[var(--leros-primary)] px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90">
							<FileText className="size-4" />
							上传
							<input
								type="file"
								className="hidden"
								accept={PROJECT_ATTACHMENT_ACCEPT}
								onChange={handleUpload}
								disabled={uploading}
							/>
						</label>
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

				<div className="overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-white">
					<div className="grid grid-cols-[minmax(0,1fr)_120px_180px_220px] border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-6 py-4 text-xs font-semibold uppercase tracking-wider text-[var(--leros-text-muted)]">
						<div>名称</div>
						<div>大小</div>
						<div>上传时间</div>
						<div className="text-right">操作</div>
					</div>

					{filteredFiles.length === 0 ? (
						<div className="px-6 py-16 text-center text-sm text-[var(--leros-text-muted)]">
							暂无文件
						</div>
					) : (
						<div className="divide-y divide-[var(--leros-control-border)]/60">
							{filteredFiles.map((file) => (
								<div
									key={file.path}
									className="grid grid-cols-[minmax(0,1fr)_120px_180px_220px] items-center px-6 py-5 transition-colors hover:bg-[var(--leros-primary-softer)]/25"
								>
									<button
										type="button"
										data-file-preview-trigger
										onClick={() => setPreviewFile(file)}
										className="flex min-w-0 cursor-pointer items-center gap-3 rounded-lg px-2 py-1 text-left transition-colors hover:bg-[var(--leros-primary-softer)]/50"
										title="查看"
									>
										<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
											<ProjectFileIcon file={file} />
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
									<div className="text-sm text-[var(--leros-text-muted)]">
										{formatBytes(file.size)}
									</div>
									<div className="text-sm text-[var(--leros-text-muted)]">
										{formatTime(file.modTime)}
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
					)}
				</div>
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

function ProjectFileIcon({ file }: { file: ProjectFileNode }) {
	const lowerPath = file.name.toLowerCase();
	let iconSrc = "/assets/icons/file-text.svg";

	if (lowerPath.endsWith(".jpg")) {
		iconSrc = "/assets/icons/file-picture-jpg.svg";
	} else if (lowerPath.endsWith(".jpeg")) {
		iconSrc = "/assets/icons/file-picture-jpeg.svg";
	} else if (lowerPath.endsWith(".png")) {
		iconSrc = "/assets/icons/file-picture-png.svg";
	} else if (lowerPath.endsWith(".pdf")) {
		iconSrc = "/assets/icons/file-pdf.svg";
	} else if (lowerPath.endsWith(".json")) {
		iconSrc = "/assets/icons/file-text.svg";
	} else if (
		lowerPath.endsWith(".csv") ||
		lowerPath.endsWith(".xlsx") ||
		lowerPath.endsWith(".xls")
	) {
		iconSrc = "/assets/icons/file-text.svg";
	}

	return <img src={iconSrc} alt="" className="size-6 object-contain" aria-hidden="true" />;
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

function ProjectArtifactList({
	artifacts,
	emptyText = "暂无产物",
	compact = false,
}: {
	artifacts: ProjectArtifact[];
	emptyText?: string;
	compact?: boolean;
}) {
	const [previewArtifact, setPreviewArtifact] = useState<ProjectArtifact | null>(null);

	if (artifacts.length === 0) {
		return (
			<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]">
				{emptyText}
			</div>
		);
	}

	return (
		<>
			<div className={cn("w-full", compact ? "mx-auto max-w-[250px] space-y-3" : "space-y-3")}>
				{artifacts.map((artifact) => (
					<button
						type="button"
						key={artifact.id}
						onClick={() => setPreviewArtifact(artifact)}
						className={cn(
							"group relative flex w-full cursor-pointer items-center overflow-hidden border border-[var(--leros-control-border)] bg-[var(--leros-surface)] text-left shadow-sm transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35",
							compact ? "gap-3 rounded-lg px-3.5 py-3" : "gap-3.5 rounded-lg px-4 py-3.5",
						)}
						title="预览产物"
					>
						{/* hover 时补一个轻量蒙层，明确提示当前整卡可点击预览 */}
						<div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover:opacity-100">
							<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
								点击预览
							</span>
						</div>
						<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-text)]">
							<ArtifactIcon type={artifact.type} />
						</div>
						<div className="min-w-0">
							<div className="truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
								{artifact.name}
							</div>
							<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
								{artifact.size}
								{artifact.updatedAt ? ` · ${artifact.updatedAt}` : ""}
							</div>
						</div>
					</button>
				))}
			</div>
			<ArtifactPreviewDialog
				artifact={previewArtifact}
				open={previewArtifact !== null}
				onOpenChange={(open) => {
					if (!open) setPreviewArtifact(null);
				}}
			/>
		</>
	);
}

function ArtifactIcon({ type }: { type: ProjectArtifact["type"] }) {
	const className = "size-4";

	switch (type) {
		case "spreadsheet":
			return <Table2 className={className} />;
		case "image":
			return <FileImage className={className} />;
		default:
			return <FileText className={className} />;
	}
}
