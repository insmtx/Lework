"use client";

import type { Project, ProjectArtifact, ProjectTask } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import {
	Bot,
	CheckCircle2,
	FileImage,
	FileText,
	LayoutPanelLeft,
	Search,
	Settings,
	Table2,
} from "lucide-react";
import { MessageTimeline } from "../chat/MessageTimeline";
import { ChatInput } from "../input/ChatInput";

const projectTabs = [
	{ id: "chat" as const, label: "会话" },
	{ id: "tasks" as const, label: "任务" },
	{ id: "files" as const, label: "文件" },
	{ id: "memory" as const, label: "记忆" },
];

export function ProjectPage() {
	const { projects, activeProjectId, activeProjectTab, switchView, setActiveProjectTab } =
		useLayoutStore((s) => s);

	const project = projects.find((item) => item.id === activeProjectId) ?? projects[0];

	if (!project) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[#f7f8fd] text-slate-500">
				暂无项目
			</div>
		);
	}

	return (
		<div data-slot="project-page" className="flex h-full flex-1 flex-col bg-white">
			<header className="flex h-[78px] shrink-0 items-center justify-between border-b border-slate-200 bg-[#f8f9fe] px-8">
				<div className="flex items-center gap-3 text-slate-500">
					<button
						type="button"
						onClick={() => switchView("workbench")}
						className="text-sm font-medium hover:text-slate-800"
					>
						Projects
					</button>
					<span>›</span>
					<h1 className="text-2xl font-bold text-slate-950">{project.name}</h1>
				</div>
				<div className="flex items-center gap-8 text-slate-600">
					<Search className="size-6" />
					<LayoutPanelLeft className="size-6" />
					<Settings className="size-6" />
				</div>
			</header>

			<nav className="flex h-[64px] shrink-0 items-end gap-10 border-b border-slate-200 bg-[#f8f9fe] px-8">
				{projectTabs.map((tab) => (
					<button
						key={tab.id}
						type="button"
						onClick={() => setActiveProjectTab(tab.id)}
						className={cn(
							"relative h-full px-1 text-base font-medium transition-colors",
							activeProjectTab === tab.id ? "text-blue-600" : "text-slate-600 hover:text-slate-950",
						)}
					>
						{tab.label}
						{activeProjectTab === tab.id && (
							<span className="absolute bottom-0 left-0 h-0.5 w-full rounded-full bg-blue-600" />
						)}
					</button>
				))}
			</nav>

			<div className="min-h-0 flex flex-1">
				<main
					className={cn(
						"min-w-0 flex-1",
						activeProjectTab === "chat"
							? "flex min-h-0 flex-col bg-white"
							: "overflow-y-auto px-8 py-8",
					)}
				>
					{activeProjectTab === "chat" && <ProjectChat />}
					{activeProjectTab === "tasks" && <ProjectTasks tasks={project.tasks} />}
					{activeProjectTab === "files" && <ProjectFiles files={project.files} />}
					{activeProjectTab === "memory" && <ProjectMemories project={project} />}
				</main>

				<aside className="flex w-[360px] shrink-0 flex-col border-l border-slate-200 bg-[#f8f9fe] px-7 py-8">
					<div className="min-h-0 flex-1 space-y-8 overflow-y-auto">
						<section>
							<div className="mb-4 flex items-center justify-between">
								<h2 className="text-sm font-semibold tracking-wide text-slate-600">任务</h2>
								<span className="rounded bg-blue-100 px-2 py-0.5 text-xs font-semibold text-blue-700">
									{project.tasks.length} 项
								</span>
							</div>
							<ProjectTaskList tasks={project.tasks} compact />
						</section>

						<section>
							<div className="mb-4 flex items-center justify-between">
								<h2 className="text-sm font-semibold tracking-wide text-slate-600">产物</h2>
								<span className="rounded bg-slate-200 px-2 py-0.5 text-xs font-semibold text-slate-600">
									{project.artifacts.length} 个
								</span>
							</div>
							<ProjectArtifactList artifacts={project.artifacts} />
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
				<div className="flex size-12 items-center justify-center rounded-full bg-blue-50 text-blue-600">
					<Bot className="size-6" />
				</div>
				<h2 className="mt-5 text-lg font-semibold text-slate-800">开始项目会话</h2>
				<p className="mt-2 text-sm leading-6 text-slate-500">
					把需求、问题或上下文发给 AI，后续讨论会沉淀在当前项目中。
				</p>
			</div>
		</div>
	);
}

function ProjectTasks({ tasks }: { tasks: ProjectTask[] }) {
	return (
		<div className="max-w-3xl">
			<h2 className="text-2xl font-bold text-slate-950">任务</h2>
			<div className="mt-6">
				<ProjectTaskList tasks={tasks} />
			</div>
		</div>
	);
}

function ProjectTaskList({ tasks, compact = false }: { tasks: ProjectTask[]; compact?: boolean }) {
	return (
		<div className="space-y-4">
			{tasks.map((task) => (
				<div
					key={task.id}
					className={cn(
						"flex items-start gap-4 rounded-xl border border-slate-200 bg-white shadow-sm",
						compact ? "px-4 py-4" : "px-5 py-5",
					)}
				>
					<CheckCircle2
						className={cn(
							"mt-1 size-5",
							task.status === "done" ? "text-green-500" : "text-slate-500",
						)}
					/>
					<div className="min-w-0">
						<div className="font-semibold text-slate-950">{task.title}</div>
						<div className="mt-1 text-sm text-slate-500">{task.meta}</div>
					</div>
				</div>
			))}
		</div>
	);
}

function ProjectFiles({ files }: { files: ProjectArtifact[] }) {
	return (
		<div className="max-w-3xl">
			<h2 className="text-2xl font-bold text-slate-950">文件</h2>
			<div className="mt-6">
				<ProjectArtifactList artifacts={files} emptyText="暂无文件" />
			</div>
		</div>
	);
}

function ProjectMemories({ project }: { project: Project }) {
	return (
		<div className="max-w-3xl">
			<h2 className="text-2xl font-bold text-slate-950">记忆</h2>
			<div className="mt-6 space-y-4">
				{project.memories.map((memory) => (
					<div
						key={memory.id}
						className="rounded-xl border border-slate-200 bg-white px-5 py-5 shadow-sm"
					>
						<div className="font-semibold text-slate-950">{memory.title}</div>
						<p className="mt-2 text-sm leading-6 text-slate-600">{memory.content}</p>
					</div>
				))}
				{project.memories.length === 0 && <div className="text-sm text-slate-500">暂无记忆</div>}
			</div>
		</div>
	);
}

function ProjectArtifactList({
	artifacts,
	emptyText = "暂无产物",
}: {
	artifacts: ProjectArtifact[];
	emptyText?: string;
}) {
	if (artifacts.length === 0) {
		return (
			<div className="rounded-xl border border-dashed border-slate-200 px-5 py-8 text-center text-sm text-slate-500">
				{emptyText}
			</div>
		);
	}

	return (
		<div className="space-y-4">
			{artifacts.map((artifact) => (
				<div
					key={artifact.id}
					className="flex items-center gap-4 rounded-xl border border-slate-200 bg-white px-4 py-4 shadow-sm"
				>
					<div className="flex size-12 shrink-0 items-center justify-center rounded-lg bg-blue-100 text-slate-600">
						<ArtifactIcon type={artifact.type} />
					</div>
					<div className="min-w-0">
						<div className="truncate font-semibold text-slate-950">{artifact.name}</div>
						<div className="mt-1 text-sm text-slate-500">
							{artifact.size} · {artifact.updatedAt}
						</div>
					</div>
				</div>
			))}
		</div>
	);
}

function ArtifactIcon({ type }: { type: ProjectArtifact["type"] }) {
	switch (type) {
		case "spreadsheet":
			return <Table2 className="size-5" />;
		case "image":
			return <FileImage className="size-5" />;
		default:
			return <FileText className="size-5" />;
	}
}
