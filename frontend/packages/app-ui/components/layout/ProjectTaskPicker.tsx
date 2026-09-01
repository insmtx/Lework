"use client";

import type { Project, ProjectTask } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import { Command, CommandInput } from "@leros/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, ChevronRight, ListTodo, LoaderCircle, Plus } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { ListLoadMoreSentinel } from "../common/ListLoadMoreSentinel";
import { renderHighlightedText } from "../common/searchText";
import { usePaginatedProjectList } from "../project/usePaginatedProjectList";
import { ProjectIcon } from "./project-icon";

export type ProjectTaskPickerProject = Pick<Project, "id" | "name" | "tasks">;

const PROJECT_PICKER_MAX_HEIGHT = "max-h-[min(420px,70vh)]";
const PROJECT_PICKER_PANEL_CLASS = cn(
	"w-[360px] overflow-hidden rounded-2xl border border-slate-200/80 bg-white/95 p-2 shadow-[0_18px_45px_rgba(15,23,42,0.16)] backdrop-blur",
	PROJECT_PICKER_MAX_HEIGHT,
);
const PROJECT_PICKER_LIST_CLASS = "no-scrollbar max-h-60 space-y-1 overflow-y-auto";
const PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX = 250;
const PROJECT_PICKER_SUBMENU_VIEWPORT_MARGIN_PX = 16;
const PROJECT_PICKER_ROW_HEIGHT_PX = 40;
const PROJECT_PICKER_SUBMENU_BLOCK_PADDING_PX = 12;
const PROJECT_PICKER_SUBMENU_PADDING_TOP_PX = 6;
const PROJECT_PICKER_SUBMENU_PANEL_CLASS =
	"no-scrollbar absolute left-[calc(100%+4px)] z-50 w-[260px] overflow-y-auto rounded-2xl border border-slate-200/80 bg-white/95 p-1.5 shadow-[0_18px_45px_rgba(15,23,42,0.16)] backdrop-blur";
const PROJECT_PICKER_HOVER_SUBMENU_MS = 500;

function getFilteredProjects(projects: ProjectTaskPickerProject[], query: string) {
	const keyword = query.trim().toLowerCase();
	if (!keyword) return projects;
	return projects.filter((project) => project.name.toLowerCase().includes(keyword));
}

function estimateSubmenuHeightForFlip(
	submenu: string,
	taskCount: number,
	showTasks: boolean,
	isLoading: boolean,
): number {
	let contentHeight = PROJECT_PICKER_ROW_HEIGHT_PX;
	if (submenu.startsWith("project:")) {
		const listCount = showTasks && taskCount > 0 ? taskCount : isLoading ? 1 : 0;
		if (listCount > 0) {
			contentHeight +=
				PROJECT_PICKER_SUBMENU_BLOCK_PADDING_PX + listCount * PROJECT_PICKER_ROW_HEIGHT_PX;
		}
	}
	return Math.min(
		PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX,
		contentHeight + PROJECT_PICKER_SUBMENU_BLOCK_PADDING_PX,
	);
}

function isRowVisibleInClip(row: HTMLElement, clip: HTMLElement | null): boolean {
	const rowRect = row.getBoundingClientRect();
	if (rowRect.height <= 0 || rowRect.width <= 0) return false;
	if (!clip) {
		return rowRect.bottom > 0 && rowRect.top < window.innerHeight;
	}
	const clipRect = clip.getBoundingClientRect();
	const overlap = Math.min(rowRect.bottom, clipRect.bottom) - Math.max(rowRect.top, clipRect.top);
	return overlap >= rowRect.height * 0.6;
}

function resolveSubmenuTop(rootRect: DOMRect, rowTop: number, submenuHeight: number): number {
	const alignedTop = rowTop - PROJECT_PICKER_SUBMENU_PADDING_TOP_PX;
	const submenuScreenTop = rootRect.top + alignedTop;
	const viewportTop = PROJECT_PICKER_SUBMENU_VIEWPORT_MARGIN_PX;
	const viewportBottom = window.innerHeight - PROJECT_PICKER_SUBMENU_VIEWPORT_MARGIN_PX;
	const submenuScreenBottom = submenuScreenTop + submenuHeight;

	if (submenuScreenBottom <= viewportBottom) {
		return alignedTop;
	}

	const flippedTop = alignedTop - (submenuScreenBottom - viewportBottom);
	const minTop = viewportTop - rootRect.top;
	return Math.max(minTop, flippedTop);
}

function projectPickerRowClass(selected: boolean) {
	return cn(
		"flex h-10 w-full items-center gap-2.5 rounded-xl px-3 text-left text-sm font-medium transition-colors",
		selected
			? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)] ring-1 ring-[var(--leros-primary-soft)]"
			: "text-slate-700 hover:bg-slate-100",
	);
}

function projectPickerPlaceholderRowClass() {
	return cn(projectPickerRowClass(false), "pointer-events-none text-slate-400");
}

function NewTaskPickerRow({ selected, onClick }: { selected: boolean; onClick: () => void }) {
	return (
		<button type="button" onClick={onClick} className={projectPickerRowClass(selected)}>
			<Plus className="size-4 shrink-0" />
			<span className="min-w-0 flex-1 truncate">新建任务</span>
			{selected ? <Check className="size-4 shrink-0" /> : null}
		</button>
	);
}

export function formatProjectTaskPickerLabel(
	projects: ProjectTaskPickerProject[],
	projectId?: string | null,
	taskId?: string | null,
	emptyLabel = "新建项目/任务",
): string {
	const project = projects.find((item) => item.id === projectId);
	if (!project) return emptyLabel;
	const task = project.tasks.find((item) => item.id === taskId);
	return task ? `${project.name} / ${task.title}` : `${project.name} / 新建任务`;
}

export type ProjectTaskPickerContentProps = {
	projects?: ProjectTaskPickerProject[];
	listLoading?: boolean;
	hasMore?: boolean;
	loadingMore?: boolean;
	onLoadMore?: () => void;
	/** When false, skip fetching even if `projects` is omitted. */
	listEnabled?: boolean;
	selectedProjectId?: string | null;
	selectedTaskId?: string | null;
	searchQuery: string;
	onSearchQueryChange: (value: string) => void;
	allowNewProject?: boolean;
	/** 为 false 时只选项目（隐含新建任务），不展示已有任务侧边栏。 */
	allowSelectTask?: boolean;
	onSelectProject: (project: ProjectTaskPickerProject) => void;
	onSelectTask: (project: ProjectTaskPickerProject, task: ProjectTask) => void;
	onSelectNewProject?: () => void;
	onLoadProjectTasks?: (projectId: string) => void | Promise<void>;
	/** 打开时滚动到当前选中项目 */
	scrollSelectedIntoView?: boolean;
};

/** 中文注释：工作台与标书对比共用的项目/任务选择面板，含右侧二级任务侧边栏。 */
export function ProjectTaskPickerContent({
	projects: projectsProp,
	listLoading: listLoadingProp,
	hasMore: hasMoreProp,
	loadingMore: loadingMoreProp,
	onLoadMore: onLoadMoreProp,
	listEnabled = true,
	selectedProjectId,
	selectedTaskId,
	searchQuery,
	onSearchQueryChange,
	allowNewProject = true,
	allowSelectTask = true,
	onSelectProject,
	onSelectTask,
	onSelectNewProject,
	onLoadProjectTasks,
	scrollSelectedIntoView = false,
}: ProjectTaskPickerContentProps) {
	const pickerRootRef = useRef<HTMLDivElement>(null);
	const [listRoot, setListRoot] = useState<HTMLDivElement | null>(null);
	const submenuRowRef = useRef<HTMLElement | null>(null);
	const [hoveredSubmenu, setHoveredSubmenu] = useState<"new-project" | `project:${string}` | null>(
		null,
	);
	const [submenuTop, setSubmenuTop] = useState(0);
	const [loadingTaskProjectIds, setLoadingTaskProjectIds] = useState<Set<string>>(() => new Set());
	const [revealedTaskProjectId, setRevealedTaskProjectId] = useState<string | null>(null);
	const submenuTimerRef = useRef<number | null>(null);
	const hoverSessionRef = useRef(0);
	const lastPointerRef = useRef({ x: 0, y: 0 });
	const scrollSettleTimerRef = useRef<number | null>(null);

	const clearSubmenuTimer = useCallback(() => {
		if (submenuTimerRef.current == null) return;
		window.clearTimeout(submenuTimerRef.current);
		submenuTimerRef.current = null;
	}, []);

	const beginHoverSession = useCallback(() => {
		hoverSessionRef.current += 1;
		clearSubmenuTimer();
		setRevealedTaskProjectId(null);
	}, [clearSubmenuTimer]);
	const shouldFetchList = projectsProp == null && listEnabled;
	const projectList = usePaginatedProjectList({
		enabled: shouldFetchList,
		keyword: searchQuery,
	});
	const projects = projectsProp ?? projectList.projects;
	const visibleProjects = projectsProp ? getFilteredProjects(projects, searchQuery) : projects;
	const listLoading = shouldFetchList ? projectList.loading : Boolean(listLoadingProp);
	const listHasMore = shouldFetchList ? projectList.hasMore : Boolean(hasMoreProp);
	const listLoadingMore = shouldFetchList ? projectList.loadingMore : Boolean(loadingMoreProp);
	const onLoadMore = shouldFetchList ? projectList.loadMore : onLoadMoreProp;
	const hoveredProject =
		hoveredSubmenu?.startsWith("project:") === true
			? projects.find((project) => project.id === hoveredSubmenu.slice("project:".length))
			: undefined;
	const hoveredProjectTaskLoading = hoveredProject
		? loadingTaskProjectIds.has(hoveredProject.id)
		: false;
	const showHoveredProjectTasks =
		Boolean(hoveredProject) &&
		revealedTaskProjectId === hoveredProject?.id &&
		!hoveredProjectTaskLoading;
	const hasProjectSelection = Boolean(selectedProjectId);

	const loadProjectTasksIfNeeded = useCallback(
		(projectId: string, session: number) => {
			if (!onLoadProjectTasks || loadingTaskProjectIds.has(projectId)) {
				return;
			}
			setLoadingTaskProjectIds((current) => new Set(current).add(projectId));
			void Promise.resolve(onLoadProjectTasks(projectId)).finally(() => {
				setLoadingTaskProjectIds((current) => {
					const next = new Set(current);
					next.delete(projectId);
					return next;
				});
				if (session !== hoverSessionRef.current) return;
				setRevealedTaskProjectId(projectId);
			});
		},
		[loadingTaskProjectIds, onLoadProjectTasks],
	);

	const hideHoveredProject = useCallback(() => {
		beginHoverSession();
		setHoveredSubmenu(null);
		setSubmenuTop(0);
		submenuRowRef.current = null;
	}, [beginHoverSession]);

	const openSubmenu = useCallback(
		(row: HTMLElement, submenu: "new-project" | `project:${string}`, session: number) => {
			const clip = listRoot?.contains(row) ? listRoot : null;
			if (!isRowVisibleInClip(row, clip)) return;
			const root = pickerRootRef.current;
			submenuRowRef.current = row;
			setHoveredSubmenu(submenu);
			if (root) {
				const rootRect = root.getBoundingClientRect();
				const rowTop = row.getBoundingClientRect().top - rootRect.top;
				const isProject = submenu.startsWith("project:");
				const submenuHeight = estimateSubmenuHeightForFlip(submenu, 0, false, isProject);
				setSubmenuTop(resolveSubmenuTop(rootRect, rowTop, submenuHeight));
			}
			if (submenu.startsWith("project:")) {
				loadProjectTasksIfNeeded(submenu.slice("project:".length), session);
			}
		},
		[listRoot, loadProjectTasksIfNeeded],
	);

	const showSubmenuAtRow = useCallback(
		(row: HTMLElement, submenu: "new-project" | `project:${string}`) => {
			if (submenu.startsWith("project:") && !allowSelectTask) {
				hideHoveredProject();
				return;
			}
			if (hoveredSubmenu === submenu) return;
			hideHoveredProject();
			const session = hoverSessionRef.current;
			submenuRowRef.current = row;
			submenuTimerRef.current = window.setTimeout(() => {
				submenuTimerRef.current = null;
				if (session !== hoverSessionRef.current) return;
				openSubmenu(row, submenu, session);
			}, PROJECT_PICKER_HOVER_SUBMENU_MS);
		},
		[allowSelectTask, hideHoveredProject, hoveredSubmenu, openSubmenu],
	);

	useEffect(() => () => clearSubmenuTimer(), [clearSubmenuTimer]);

	useEffect(() => {
		if (!listRoot) return;
		const onScroll = () => {
			const row = submenuRowRef.current;
			if (!row || !listRoot.contains(row)) return;
			hideHoveredProject();
			if (scrollSettleTimerRef.current != null) {
				window.clearTimeout(scrollSettleTimerRef.current);
			}
			scrollSettleTimerRef.current = window.setTimeout(() => {
				scrollSettleTimerRef.current = null;
				const { x, y } = lastPointerRef.current;
				const el = document.elementFromPoint(x, y);
				if (!(el instanceof Element)) return;
				const item = el.closest("[data-project-picker-item]");
				if (!(item instanceof HTMLElement)) return;
				const projectId = item.getAttribute("data-project-picker-item");
				if (!projectId) return;
				showSubmenuAtRow(item, `project:${projectId}`);
			}, 80);
		};
		listRoot.addEventListener("scroll", onScroll, { passive: true });
		return () => {
			listRoot.removeEventListener("scroll", onScroll);
			if (scrollSettleTimerRef.current != null) {
				window.clearTimeout(scrollSettleTimerRef.current);
			}
		};
	}, [hideHoveredProject, listRoot, showSubmenuAtRow]);

	useEffect(() => {
		const row = submenuRowRef.current;
		const root = pickerRootRef.current;
		if (!row || !root || !hoveredSubmenu) return;
		if (listRoot?.contains(row) && !isRowVisibleInClip(row, listRoot)) {
			hideHoveredProject();
			return;
		}

		const projectId = hoveredSubmenu.startsWith("project:")
			? hoveredSubmenu.slice("project:".length)
			: null;
		const showTasks = projectId != null && revealedTaskProjectId === projectId;
		const isLoading = projectId != null && loadingTaskProjectIds.has(projectId);
		const taskCount = showTasks
			? (projects.find((item) => item.id === projectId)?.tasks.length ?? 0)
			: 0;
		const rootRect = root.getBoundingClientRect();
		const rowTop = row.getBoundingClientRect().top - rootRect.top;
		const submenuHeight = estimateSubmenuHeightForFlip(
			hoveredSubmenu,
			taskCount,
			showTasks,
			isLoading,
		);
		setSubmenuTop(resolveSubmenuTop(rootRect, rowTop, submenuHeight));
		// 中文注释：仅在切换 hover 目标时重算位置；任务列表刷新/首次加载只向下展开，避免 submenuTop 跳变导致底部抖动。
	}, [hoveredSubmenu]);

	const projectListRefCallback = useCallback(
		(node: HTMLDivElement | null) => {
			if (!node || !scrollSelectedIntoView || !selectedProjectId) return;
			const item = node.querySelector<HTMLElement>(
				`[data-project-picker-item="${CSS.escape(selectedProjectId)}"]`,
			);
			item?.scrollIntoView({ block: "center", behavior: "instant" });
		},
		[scrollSelectedIntoView, selectedProjectId],
	);

	const handleSearchChange = (value: string) => {
		onSearchQueryChange(value);
		hideHoveredProject();
	};

	return (
		<div
			ref={pickerRootRef}
			className="relative"
			onPointerMove={(event) => {
				lastPointerRef.current = { x: event.clientX, y: event.clientY };
			}}
		>
			<div className={PROJECT_PICKER_PANEL_CLASS}>
				<Command
					shouldFilter={false}
					className="rounded-xl! bg-transparent p-0 [&_[data-slot=command-input-wrapper]]:p-0"
				>
					<CommandInput
						value={searchQuery}
						onValueChange={handleSearchChange}
						placeholder="搜索项目"
						className="placeholder:text-slate-300"
					/>
				</Command>
				{allowNewProject ? (
					<div className="mt-1 mb-1">
						<button
							type="button"
							data-project-picker-new=""
							onMouseEnter={(event) => showSubmenuAtRow(event.currentTarget, "new-project")}
							onClick={() => onSelectNewProject?.()}
							className={projectPickerRowClass(!hasProjectSelection)}
						>
							<Plus className="size-4 shrink-0" />
							<span className="min-w-0 flex-1 truncate">新建项目</span>
							{!hasProjectSelection ? <Check className="size-4 shrink-0" /> : null}
							<ChevronRight className="size-3.5 shrink-0 text-slate-400" />
						</button>
					</div>
				) : null}
				<div
					ref={(node) => {
						setListRoot(node);
						projectListRefCallback(node);
					}}
					className={cn(
						PROJECT_PICKER_LIST_CLASS,
						allowNewProject ? "border-t border-slate-100 pt-1" : "mt-1",
					)}
				>
					{listLoading && visibleProjects.length === 0 ? (
						<div className="px-3 py-8 text-center text-sm text-slate-400">正在加载项目...</div>
					) : null}
					{visibleProjects.map((project) => {
						const projectSelected = selectedProjectId === project.id;
						return (
							<button
								key={project.id}
								type="button"
								data-project-picker-item={project.id}
								onMouseEnter={(event) =>
									showSubmenuAtRow(event.currentTarget, `project:${project.id}`)
								}
								onClick={() => onSelectProject(project)}
								className={projectPickerRowClass(projectSelected)}
							>
								<ProjectIcon className="size-4 shrink-0" />
								<span className="min-w-0 flex-1 truncate">
									{renderHighlightedText(project.name, searchQuery)}
								</span>
								{projectSelected ? <Check className="size-4 shrink-0" /> : null}
								{allowSelectTask ? (
									<ChevronRight className="size-3.5 shrink-0 text-slate-400" />
								) : null}
							</button>
						);
					})}
					{!listLoading && visibleProjects.length === 0 ? (
						<div className="px-3 py-8 text-center text-sm text-slate-400">没有匹配的项目</div>
					) : null}
					{onLoadMore ? (
						<ListLoadMoreSentinel
							hasMore={listHasMore}
							loading={listLoadingMore}
							onLoadMore={onLoadMore}
							root={listRoot}
							className="py-2"
						/>
					) : null}
				</div>
			</div>

			{allowNewProject && hoveredSubmenu === "new-project" ? (
				<div
					className={PROJECT_PICKER_SUBMENU_PANEL_CLASS}
					style={{ top: submenuTop, maxHeight: PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX }}
				>
					<NewTaskPickerRow
						selected={!hasProjectSelection}
						onClick={() => onSelectNewProject?.()}
					/>
				</div>
			) : null}

			{allowSelectTask && hoveredProject ? (
				<div
					className={PROJECT_PICKER_SUBMENU_PANEL_CLASS}
					style={{ top: submenuTop, maxHeight: PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX }}
				>
					<NewTaskPickerRow
						selected={selectedProjectId === hoveredProject.id && !selectedTaskId}
						onClick={() => onSelectProject(hoveredProject)}
					/>
					{hoveredProjectTaskLoading ? (
						<div className="space-y-1 border-t border-slate-100 pt-1">
							<div className={projectPickerPlaceholderRowClass()}>
								<LoaderCircle className="size-4 shrink-0 animate-spin opacity-75" />
								<span className="min-w-0 flex-1 truncate">任务加载中...</span>
							</div>
						</div>
					) : showHoveredProjectTasks && hoveredProject.tasks.length > 0 ? (
						<div className="space-y-1 border-t border-slate-100 pt-1">
							{hoveredProject.tasks.map((task) => {
								const isResponding = task.runtimeStatus === "responding";
								const selected =
									!isResponding &&
									selectedProjectId === hoveredProject.id &&
									selectedTaskId === task.id;
								return (
									<button
										key={task.id}
										type="button"
										disabled={isResponding}
										onClick={() => onSelectTask(hoveredProject, task)}
										className={cn(
											projectPickerRowClass(selected),
											isResponding && "cursor-not-allowed opacity-60",
										)}
									>
										<ListTodo className="size-4 shrink-0 opacity-75" />
										<span className="min-w-0 flex-1 truncate">{task.title}</span>
										{isResponding ? (
											<span className="shrink-0 text-xs text-slate-400">回复中</span>
										) : (
											selected && <Check className="size-4 shrink-0" />
										)}
									</button>
								);
							})}
						</div>
					) : null}
				</div>
			) : null}
		</div>
	);
}

export type ProjectTaskPickerFieldProps = {
	projectId?: string | null;
	taskId?: string | null;
	disabled?: boolean;
	allowNewProject?: boolean;
	/** 为 false 时只选项目（隐含新建任务），不可选已有任务。 */
	allowSelectTask?: boolean;
	onSelect: (projectId: string, taskId: string) => void;
	onLoadProjectTasks?: (projectId: string) => void | Promise<void>;
	className?: string;
};

/** 中文注释：表单场景的项目/任务下拉，复用与工作台相同的选择面板。 */
export function ProjectTaskPickerField({
	projectId,
	taskId,
	disabled,
	allowNewProject = true,
	allowSelectTask = true,
	onSelect,
	onLoadProjectTasks,
	className,
}: ProjectTaskPickerFieldProps) {
	const [open, setOpen] = useState(false);
	const [searchQuery, setSearchQuery] = useState("");
	const projectList = usePaginatedProjectList({
		enabled: true,
		keyword: searchQuery,
	});
	const cachedProjects = useLayoutStore((state) => state.projects);
	const label = formatProjectTaskPickerLabel(
		cachedProjects,
		projectId,
		allowSelectTask ? taskId : null,
	);

	const close = () => {
		setOpen(false);
		setSearchQuery("");
	};

	return (
		<div className={className}>
			<div className="mb-2 text-sm font-semibold text-slate-800">
				{allowSelectTask ? "选择项目 / 任务" : "选择项目 / 新建任务"}{" "}
				<span className="text-red-500">*</span>
			</div>
			<Popover
				open={open}
				onOpenChange={(next) => {
					setOpen(next);
					if (!next) setSearchQuery("");
				}}
			>
				<PopoverTrigger
					type="button"
					disabled={disabled}
					className="flex h-10 w-full min-w-0 items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-left text-sm text-slate-700 shadow-none transition-colors hover:border-[var(--leros-primary)]/40 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
				>
					<ProjectIcon className="size-4 shrink-0" />
					<span className="min-w-0 flex-1 truncate">{label}</span>
					<ChevronDown className="size-4 shrink-0 text-slate-400" />
				</PopoverTrigger>
				<PopoverContent
					align="start"
					collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
					className="z-[70] !flex-none w-auto overflow-visible rounded-none border-0 bg-transparent p-0 shadow-none ring-0"
				>
					<ProjectTaskPickerContent
						projects={projectList.projects}
						listLoading={projectList.loading}
						hasMore={projectList.hasMore}
						loadingMore={projectList.loadingMore}
						onLoadMore={projectList.loadMore}
						selectedProjectId={projectId}
						selectedTaskId={allowSelectTask ? taskId : null}
						searchQuery={searchQuery}
						onSearchQueryChange={setSearchQuery}
						allowNewProject={allowNewProject}
						allowSelectTask={allowSelectTask}
						onLoadProjectTasks={allowSelectTask ? onLoadProjectTasks : undefined}
						scrollSelectedIntoView={open}
						onSelectProject={(project) => {
							onSelect(project.id, "");
							close();
						}}
						onSelectTask={(project, task) => {
							if (!allowSelectTask) return;
							onSelect(project.id, task.id);
							close();
						}}
						onSelectNewProject={() => {
							onSelect("", "");
							close();
						}}
					/>
				</PopoverContent>
			</Popover>
		</div>
	);
}
