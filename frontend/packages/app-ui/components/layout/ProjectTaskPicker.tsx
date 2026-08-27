"use client";

import type { Project, ProjectTask } from "@leros/store";
import { Command, CommandInput } from "@leros/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import { Check, ChevronDown, ChevronRight, ListTodo, LoaderCircle, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { renderHighlightedText } from "../common/searchText";
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

function getFilteredProjects(projects: ProjectTaskPickerProject[], query: string) {
	const keyword = query.trim().toLowerCase();
	if (!keyword) return projects;
	return projects.filter((project) => project.name.toLowerCase().includes(keyword));
}

function estimateSubmenuHeightForFlip(
	submenu: string,
	project: ProjectTaskPickerProject | undefined,
	isLoadingTasks: boolean,
): number {
	// 中文注释：右侧始终含「新建任务」一行；已有任务时再加分割线与任务行。
	let contentHeight = PROJECT_PICKER_ROW_HEIGHT_PX;
	if (submenu.startsWith("project:")) {
		const taskCount = project?.tasks.length ?? 0;
		if (taskCount > 0) {
			contentHeight +=
				PROJECT_PICKER_SUBMENU_BLOCK_PADDING_PX + taskCount * PROJECT_PICKER_ROW_HEIGHT_PX;
		} else if (isLoadingTasks) {
			contentHeight += PROJECT_PICKER_ROW_HEIGHT_PX;
		}
	}
	return Math.min(
		PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX,
		contentHeight + PROJECT_PICKER_SUBMENU_BLOCK_PADDING_PX,
	);
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
	return cn(projectPickerRowClass(false), "pointer-events-none text-xs text-slate-400");
}

function NewTaskPickerRow({
	selected,
	onClick,
}: {
	selected: boolean;
	onClick: () => void;
}) {
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
	projects: ProjectTaskPickerProject[];
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
	projects,
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
	const submenuRowRef = useRef<HTMLElement | null>(null);
	const [hoveredSubmenu, setHoveredSubmenu] = useState<"new-project" | `project:${string}` | null>(
		null,
	);
	const [submenuTop, setSubmenuTop] = useState(0);
	const [loadingTaskProjectIds, setLoadingTaskProjectIds] = useState<Set<string>>(() => new Set());

	const filteredProjects = useMemo(
		() => getFilteredProjects(projects, searchQuery),
		[projects, searchQuery],
	);
	const hoveredProject =
		hoveredSubmenu?.startsWith("project:") === true
			? projects.find((project) => project.id === hoveredSubmenu.slice("project:".length))
			: undefined;
	const hoveredProjectTaskLoading = hoveredProject
		? loadingTaskProjectIds.has(hoveredProject.id)
		: false;
	const hasProjectSelection = Boolean(selectedProjectId);

	const loadProjectTasksIfNeeded = useCallback(
		(projectId: string) => {
			const project = projects.find((item) => item.id === projectId);
			if (!project || !onLoadProjectTasks || loadingTaskProjectIds.has(projectId)) {
				return;
			}
			setLoadingTaskProjectIds((current) => new Set(current).add(projectId));
			void Promise.resolve(onLoadProjectTasks(projectId)).finally(() => {
				setLoadingTaskProjectIds((current) => {
					const next = new Set(current);
					next.delete(projectId);
					return next;
				});
			});
		},
		[loadingTaskProjectIds, onLoadProjectTasks, projects],
	);

	const showSubmenuAtRow = useCallback(
		(row: HTMLElement, submenu: "new-project" | `project:${string}`) => {
			if (submenu.startsWith("project:") && !allowSelectTask) {
				setHoveredSubmenu(null);
				setSubmenuTop(0);
				submenuRowRef.current = null;
				return;
			}
			const root = pickerRootRef.current;
			submenuRowRef.current = row;
			setHoveredSubmenu(submenu);
			if (root) {
				const projectId = submenu.startsWith("project:") ? submenu.slice("project:".length) : null;
				const project = projectId ? projects.find((item) => item.id === projectId) : undefined;
				const isLoadingTasks = projectId ? loadingTaskProjectIds.has(projectId) : false;
				const rootRect = root.getBoundingClientRect();
				const rowTop = row.getBoundingClientRect().top - rootRect.top;
				const submenuHeight = estimateSubmenuHeightForFlip(submenu, project, isLoadingTasks);
				setSubmenuTop(resolveSubmenuTop(rootRect, rowTop, submenuHeight));
			}
			if (submenu.startsWith("project:")) {
				loadProjectTasksIfNeeded(submenu.slice("project:".length));
			}
		},
		[allowSelectTask, loadProjectTasksIfNeeded, loadingTaskProjectIds, projects],
	);

	useEffect(() => {
		const row = submenuRowRef.current;
		const root = pickerRootRef.current;
		if (!row || !root || !hoveredSubmenu) return;

		const projectId = hoveredSubmenu.startsWith("project:")
			? hoveredSubmenu.slice("project:".length)
			: null;
		const project = projectId ? projects.find((item) => item.id === projectId) : undefined;
		const isLoadingTasks = projectId ? loadingTaskProjectIds.has(projectId) : false;
		const rootRect = root.getBoundingClientRect();
		const rowTop = row.getBoundingClientRect().top - rootRect.top;
		const submenuHeight = estimateSubmenuHeightForFlip(hoveredSubmenu, project, isLoadingTasks);
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
		setHoveredSubmenu(null);
		setSubmenuTop(0);
		submenuRowRef.current = null;
	};

	return (
		<div ref={pickerRootRef} className="relative">
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
					ref={projectListRefCallback}
					className={cn(
						PROJECT_PICKER_LIST_CLASS,
						allowNewProject ? "border-t border-slate-100 pt-1" : "mt-1",
					)}
				>
					{filteredProjects.map((project) => {
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
					{filteredProjects.length === 0 ? (
						<div className="px-3 py-8 text-center text-sm text-slate-400">没有匹配的项目</div>
					) : null}
				</div>
			</div>

			{allowNewProject && hoveredSubmenu === "new-project" ? (
				<div
					className={PROJECT_PICKER_SUBMENU_PANEL_CLASS}
					style={{ top: submenuTop, maxHeight: PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX }}
				>
					<NewTaskPickerRow selected={!hasProjectSelection} onClick={() => onSelectNewProject?.()} />
				</div>
			) : null}

			{allowSelectTask && hoveredProject ? (
				<div
					className={PROJECT_PICKER_SUBMENU_PANEL_CLASS}
					style={{ top: submenuTop, maxHeight: PROJECT_PICKER_SUBMENU_MAX_HEIGHT_PX }}
				>
					<div className="relative">
						<div className="space-y-1">
							<NewTaskPickerRow
								selected={selectedProjectId === hoveredProject.id && !selectedTaskId}
								onClick={() => onSelectProject(hoveredProject)}
							/>
							{hoveredProjectTaskLoading && hoveredProject.tasks.length === 0 ? (
								<div className={projectPickerPlaceholderRowClass()}>
									<LoaderCircle className="size-4 shrink-0 animate-spin opacity-75" />
									<span className="min-w-0 flex-1 truncate">任务加载中...</span>
								</div>
							) : hoveredProject.tasks.length > 0 ? (
								<div className="relative space-y-1 border-t border-slate-100 pt-1">
									<div
										className={cn(
											"space-y-1",
											hoveredProjectTaskLoading && "pointer-events-none",
										)}
									>
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
									{hoveredProjectTaskLoading ? (
										<div className="pointer-events-auto absolute inset-0 z-10 flex items-center justify-center rounded-xl bg-white/80">
											<LoaderCircle className="size-4 shrink-0 animate-spin text-slate-400" />
										</div>
									) : null}
								</div>
							) : null}
						</div>
					</div>
				</div>
			) : null}
		</div>
	);
}

export type ProjectTaskPickerFieldProps = {
	projects: ProjectTaskPickerProject[];
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
	projects,
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
	const label = formatProjectTaskPickerLabel(
		projects,
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
						projects={projects}
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
