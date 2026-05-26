"use client";

import type { ProjectArtifact, ProjectTask } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	Calendar,
	CheckCircle2,
	Circle,
	FileImage,
	FileText,
	LayoutPanelLeft,
	LoaderCircle,
	Search,
	Settings,
	Table2,
	Tag,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { MessageTimeline } from "../chat/MessageTimeline";
import { ChatInput } from "../input/ChatInput";
import { TaskDeleteDialog } from "./TaskDeleteDialog";

const projectTabs = [
	{ id: "chat" as const, label: "会话" },
	{ id: "tasks" as const, label: "任务" },
	{ id: "files" as const, label: "文件" },
];

export function ProjectPage() {
	const { projects, activeProjectId, activeProjectTab, switchView, setActiveProjectTab } =
		useLayoutStore((s) => s);

	const project = projects.find((item) => item.id === activeProjectId) ?? projects[0];

	if (!project) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-app-bg)] text-[var(--leros-text-muted)]">
				暂无项目
			</div>
		);
	}

	return (
		<div data-slot="project-page" className="flex h-full flex-1 flex-col bg-[var(--leros-surface)]">
			<header className="flex h-16 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				<div className="flex items-center gap-3 text-[var(--leros-text-muted)]">
					<button
						type="button"
						onClick={() => switchView("workbench")}
						className="text-xs font-semibold uppercase tracking-widest hover:text-[var(--leros-text-strong)]"
					>
						Projects
					</button>
					<span className="text-[var(--leros-text-subtle)]">/</span>
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
				{projectTabs.map((tab) => (
					<button
						key={tab.id}
						type="button"
						onClick={() => setActiveProjectTab(tab.id)}
						className={cn(
							"relative h-full px-1 pb-2 text-sm font-semibold transition-colors",
							activeProjectTab === tab.id
								? "text-[var(--leros-primary)]"
								: "text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]",
						)}
					>
						{tab.label}
						{activeProjectTab === tab.id && (
							<span className="absolute bottom-0 left-0 h-0.5 w-full rounded-full bg-[var(--leros-primary)]" />
						)}
					</button>
				))}
			</nav>

			<div className="min-h-0 flex flex-1">
				<main
					className={cn(
						"min-w-0 flex-1",
						activeProjectTab === "chat"
							? "flex min-h-0 flex-col bg-[var(--leros-surface)]"
							: "overflow-y-auto px-10 py-8",
					)}
				>
					{activeProjectTab === "chat" && <ProjectChat />}
					{activeProjectTab === "tasks" && <ProjectTasks tasks={project.tasks} />}
				{activeProjectTab === "files" && <ProjectFiles files={project.files} />}
				</main>

				<aside className="flex w-[300px] shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-6">
					<div className="min-h-0 flex-1 space-y-8 overflow-y-auto pr-1">
						<section>
							<div className="mx-auto mb-4 flex w-full max-w-[250px] items-center justify-between">
								<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">任务</h2>
								<span className="rounded-md bg-[var(--leros-primary-soft)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
									{project.tasks.length} 项
								</span>
							</div>
							<ProjectTaskList tasks={project.tasks} compact />
						</section>

						<section>
							<div className="mx-auto mb-4 flex w-full max-w-[250px] items-center justify-between">
								<h2 className="text-xs font-semibold text-[var(--leros-text-muted)]">产物</h2>
								<span className="rounded-md bg-[var(--leros-chat-control-bg)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-text)]">
									{project.artifacts.length} 个
								</span>
							</div>
							<ProjectArtifactList artifacts={project.artifacts} compact />
						</section>
					</div>
				</aside>
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

function ProjectTasks({ tasks }: { tasks: ProjectTask[] }) {
	const { updateTask } = useLayoutStore((s) => s);
	const [deleteTarget, setDeleteTarget] = useState<ProjectTask | null>(null);

	const handleStatusToggle = async (task: ProjectTask) => {
		await updateTask({ public_id: task.id, status: NEXT_STATUS[task.status] ?? "todo" });
	};

	return (
		<div className="mx-auto w-full max-w-[720px]">
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">任务</h2>
			<div className="mt-4">
				<ProjectTaskList tasks={tasks} onStatusToggle={handleStatusToggle} onDelete={setDeleteTarget} />
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
}: {
	tasks: ProjectTask[];
	compact?: boolean;
	onStatusToggle?: (task: ProjectTask) => void;
	onDelete?: (task: ProjectTask) => void;
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
						compact ? "gap-3 rounded-lg px-3.5 py-3" : "gap-3.5 rounded-lg px-4 py-3.5",
					)}
				>
					<button
						type="button"
						className="mt-0.5 shrink-0 cursor-pointer"
						onClick={() => onStatusToggle?.(task)}
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
					<div className="min-w-0 flex-1">
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
					</div>
					{!compact && onDelete && (
						<button
							type="button"
							className="mt-0.5 shrink-0 rounded p-0.5 text-[var(--leros-text-muted)] opacity-0 transition-opacity hover:bg-[var(--leros-danger-softer)] hover:text-[var(--leros-danger)] group-hover:opacity-100"
							onClick={() => onDelete(task)}
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

function ProjectFiles({ files }: { files: ProjectArtifact[] }) {
	return (
		<div className="mx-auto w-full max-w-[720px]">
			<h2 className="text-lg font-semibold text-[var(--leros-text-strong)]">文件</h2>
			<div className="mt-4">
				<ProjectArtifactList artifacts={files} emptyText="暂无文件" />
			</div>
		</div>
	);
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
	if (artifacts.length === 0) {
		return (
			<div className="rounded-lg border border-dashed border-[var(--leros-control-border)] px-4 py-8 text-center text-xs text-[var(--leros-text-muted)]">
				{emptyText}
			</div>
		);
	}

	return (
		<div className={cn("w-full", compact ? "mx-auto max-w-[250px] space-y-3" : "space-y-3")}>
			{artifacts.map((artifact) => (
				<div
					key={artifact.id}
					className={cn(
						"flex items-center border border-[var(--leros-control-border)] bg-[var(--leros-surface)] shadow-sm",
						compact ? "gap-3 rounded-lg px-3.5 py-3" : "gap-3.5 rounded-lg px-4 py-3.5",
					)}
				>
					<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-text)]">
						<ArtifactIcon type={artifact.type} />
					</div>
					<div className="min-w-0">
						<div className="truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
							{artifact.name}
						</div>
						<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
							{artifact.size} · {artifact.updatedAt}
						</div>
					</div>
				</div>
			))}
		</div>
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
