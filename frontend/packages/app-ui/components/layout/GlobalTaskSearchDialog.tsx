"use client";

import type { BackendTask } from "@leros/store";
import { taskApi, useLayoutStore } from "@leros/store";
import { Dialog, DialogContent, DialogTitle } from "@leros/ui/components/ui/dialog";
import { Input } from "@leros/ui/components/ui/input";
import { ScrollArea } from "@leros/ui/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@leros/ui/components/ui/select";
import { cn } from "@leros/ui/lib/utils";
import { Folder, Loader2, Search, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { renderHighlightedText } from "../common/searchText";
import type { AppNavigation } from "./LeftRail";

const ALL_PROJECTS_VALUE = "__all_projects__";
const SEARCH_LIMIT = 50;
const SEARCH_DEBOUNCE_MS = 180;

export function GlobalTaskSearchDialog({
	open,
	onOpenChange,
	navigation,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	navigation?: AppNavigation;
}) {
	const { projects, fetchProjects, openTaskDetail } = useLayoutStore((state) => ({
		projects: state.projects,
		fetchProjects: state.fetchProjects,
		openTaskDetail: state.openTaskDetail,
	}));
	const inputRef = useRef<HTMLInputElement | null>(null);
	const requestIdRef = useRef(0);
	const [selectedProjectId, setSelectedProjectId] = useState(ALL_PROJECTS_VALUE);
	const [keyword, setKeyword] = useState("");
	const [debouncedKeyword, setDebouncedKeyword] = useState("");
	const [tasks, setTasks] = useState<BackendTask[]>([]);
	const [total, setTotal] = useState(0);
	const [loading, setLoading] = useState(false);

	const selectedProjectLabel = useMemo(() => {
		if (selectedProjectId === ALL_PROJECTS_VALUE) return "全部项目";
		return projects.find((project) => project.id === selectedProjectId)?.name ?? "全部项目";
	}, [projects, selectedProjectId]);

	useEffect(() => {
		if (!open) return;
		if (projects.length === 0) {
			void fetchProjects();
		}
	}, [fetchProjects, open, projects.length]);

	useEffect(() => {
		if (!open) return;
		const timer = window.setTimeout(() => {
			setDebouncedKeyword(keyword.trim());
		}, SEARCH_DEBOUNCE_MS);
		return () => window.clearTimeout(timer);
	}, [keyword, open]);

	useEffect(() => {
		if (!open) return;

		const currentRequestId = requestIdRef.current + 1;
		requestIdRef.current = currentRequestId;
		setLoading(true);

		void taskApi
			.list({
				project_id: selectedProjectId === ALL_PROJECTS_VALUE ? undefined : selectedProjectId,
				keyword: debouncedKeyword || undefined,
				offset: 0,
				limit: SEARCH_LIMIT,
			})
			.then((response) => {
				// 中文注释：搜索输入和项目切换会同时触发请求，这里只接受最后一次返回，避免旧结果覆盖当前筛选。
				if (requestIdRef.current !== currentRequestId) return;
				const data = response.data.data;
				setTasks(data?.items ?? []);
				setTotal(data?.total ?? 0);
			})
			.catch((error) => {
				if (requestIdRef.current !== currentRequestId) return;
				console.error("GlobalTaskSearchDialog list tasks error:", error);
				setTasks([]);
				setTotal(0);
			})
			.finally(() => {
				if (requestIdRef.current === currentRequestId) {
					setLoading(false);
				}
			});
	}, [debouncedKeyword, open, selectedProjectId]);

	useEffect(() => {
		if (!open) {
			requestIdRef.current += 1;
			setKeyword("");
			setDebouncedKeyword("");
			setSelectedProjectId(ALL_PROJECTS_VALUE);
			setTasks([]);
			setTotal(0);
			setLoading(false);
			return;
		}

		const timer = window.setTimeout(() => {
			inputRef.current?.focus();
		}, 0);
		return () => window.clearTimeout(timer);
	}, [open]);

	const handleOpenTask = (task: BackendTask) => {
		onOpenChange(false);
		if (navigation) {
			navigation.goToTaskDetail(task.project_id, task.public_id, null);
			return;
		}
		openTaskDetail(task.project_id, task.public_id, null);
	};

	const hasCustomProjectFilter = selectedProjectId !== ALL_PROJECTS_VALUE;

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				showCloseButton={false}
				className="w-full max-w-[880px] overflow-hidden rounded-[28px] border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-0 shadow-[0_32px_90px_rgba(15,23,42,0.18)]"
				style={{
					minHeight: "min(56dvh, 520px)",
					maxHeight: "min(82dvh, 860px)",
				}}
			>
				<DialogTitle className="sr-only">全局任务搜索</DialogTitle>

				<div className="border-b border-[var(--leros-control-border)] px-6 pb-5 pt-5">
					<div className="grid grid-cols-[220px_minmax(0,460px)_40px] items-stretch justify-between gap-3">
						<div className="flex h-10 items-center rounded-xl border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] shadow-none transition-colors focus-within:border-[var(--leros-primary)] focus-within:ring-[3px] focus-within:ring-[var(--leros-primary)]/12">
							<Select
								value={selectedProjectId}
								onValueChange={(value) => {
									// 中文注释：Base UI Select 的 value 可能为 null，这里统一回落到“全部项目”。
									setSelectedProjectId(value ?? ALL_PROJECTS_VALUE);
								}}
							>
								<SelectTrigger className="h-full w-full rounded-xl border-0 bg-transparent px-4 text-sm font-medium text-[var(--leros-text)] shadow-none hover:border-transparent focus-visible:ring-0">
									<span className="flex min-w-0 flex-1 items-center gap-2 pr-2 text-left">
										<Folder className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
										<span className="min-w-0 truncate">{selectedProjectLabel}</span>
									</span>
								</SelectTrigger>
								<SelectContent
									align="start"
									side="bottom"
									sideOffset={8}
									// 中文注释：关闭与触发器项对齐，避免下拉层向上展开遮挡项目筛选器。
									alignItemWithTrigger={false}
									className="no-scrollbar max-h-80 min-w-[280px] overflow-y-auto rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-1.5 shadow-[0_18px_45px_rgba(30,41,59,0.18)] [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden"
								>
									<SelectItem
										value={ALL_PROJECTS_VALUE}
										className="rounded-xl px-3 py-2 text-sm font-semibold text-[var(--leros-text-strong)]"
									>
										<span className="flex min-w-0 items-center gap-2">
											<Folder className="size-4 shrink-0 text-[var(--leros-primary)]" />
											<span className="min-w-0 truncate">全部项目</span>
										</span>
									</SelectItem>
									{projects.map((project) => (
										<SelectItem
											key={project.id}
											value={project.id}
											className="rounded-xl px-3 py-2 text-sm font-medium text-[var(--leros-text)]"
										>
											<span className="flex min-w-0 items-center gap-2">
												<Folder className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
												<span className="min-w-0 truncate">{project.name}</span>
											</span>
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							{hasCustomProjectFilter ? (
								<button
									type="button"
									aria-label="清空项目筛选"
									onClick={(event) => {
										event.stopPropagation();
										setSelectedProjectId(ALL_PROJECTS_VALUE);
									}}
									className="mr-2 flex size-6 shrink-0 items-center justify-center rounded-full text-[var(--leros-text-subtle)] transition-colors hover:bg-[var(--leros-chat-control-bg)] hover:text-[var(--leros-text)]"
								>
									<X className="size-3.5" />
								</button>
							) : null}
						</div>

						<div className="flex h-10 min-w-0 items-center gap-3 rounded-xl border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-4 shadow-none transition-colors focus-within:border-[var(--leros-primary)] focus-within:ring-[3px] focus-within:ring-[var(--leros-primary)]/12">
							<Search className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
							<Input
								ref={inputRef}
								value={keyword}
								onChange={(event) => setKeyword(event.target.value)}
								placeholder="搜索任务名称"
								className="h-full border-0 bg-transparent px-0 text-sm shadow-none focus-visible:ring-0"
							/>
							{keyword ? (
								<button
									type="button"
									aria-label="清空搜索关键词"
									onClick={() => {
										setKeyword("");
										setDebouncedKeyword("");
										inputRef.current?.focus();
									}}
									className="flex size-6 shrink-0 items-center justify-center rounded-full text-[var(--leros-text-subtle)] transition-colors hover:bg-[var(--leros-chat-control-bg)] hover:text-[var(--leros-text)]"
								>
									<X className="size-3.5" />
								</button>
							) : null}
						</div>

						<button
							type="button"
							aria-label="关闭搜索弹窗"
							onClick={() => onOpenChange(false)}
							className="flex size-10 items-center justify-center rounded-xl text-[var(--leros-text-subtle)] transition-colors hover:bg-[var(--leros-chat-control-bg)] hover:text-[var(--leros-text)]"
						>
							<X className="size-5" />
						</button>
					</div>
				</div>

				<div className="px-6 pb-3 pt-4 text-sm font-medium text-[var(--leros-text-subtle)]">
					{loading
						? "正在搜索任务..."
						: total > tasks.length
							? `搜索到 ${total} 个任务，展示前 ${tasks.length} 个`
							: `搜索到 ${total} 个任务`}
				</div>

				<ScrollArea
					hideScrollbar
					className="min-h-0 px-4 pb-5"
					style={{
						minHeight: "min(calc(56dvh - 110px), 360px)",
						maxHeight: "min(calc(82dvh - 170px), 700px)",
					}}
				>
					<div className="space-y-1 px-2">
						{loading ? (
							<div className="flex h-44 items-center justify-center text-sm text-[var(--leros-text-subtle)]">
								<Loader2 className="mr-2 size-4 animate-spin" />
								正在加载搜索结果
							</div>
						) : tasks.length === 0 ? (
							<div className="flex h-44 items-center justify-center text-sm text-[var(--leros-text-subtle)]">
								没有匹配的任务
							</div>
						) : (
							tasks.map((task) => {
								const projectName = task.project_name?.trim() || "未命名项目";
								return (
									<button
										key={task.public_id}
										type="button"
										onClick={() => handleOpenTask(task)}
										className={cn(
											"flex w-full items-center gap-4 rounded-2xl px-4 py-3 text-left transition-colors",
											"hover:bg-[var(--leros-chat-control-bg)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--leros-primary)]/25",
										)}
									>
										<div className="min-w-0 flex-1">
											<div
												className="truncate text-[15px] font-semibold text-[var(--leros-text-strong)]"
												title={task.title}
											>
												{renderHighlightedText(task.title, debouncedKeyword)}
											</div>
										</div>
										<div className="flex shrink-0 items-center gap-1.5 text-xs text-[var(--leros-text-subtle)]">
											<Folder className="size-3.5 shrink-0" />
											<span
												className="max-w-[220px] truncate whitespace-nowrap"
												title={projectName}
											>
												{projectName}
											</span>
										</div>
									</button>
								);
							})
						)}
					</div>
				</ScrollArea>
			</DialogContent>
		</Dialog>
	);
}
