"use client";

import type { ProjectTask } from "@leros/store";
import { useChatStore, useLayoutStore } from "@leros/store";
import { taskApi } from "@leros/store/api/taskApi";
import { cn } from "@leros/ui/lib/utils";
import {
	ArrowLeft,
	Bot,
	Calendar,
	CheckCircle2,
	Circle,
	LoaderCircle,
	Tag,
} from "lucide-react";
import { useEffect, useState } from "react";
import { MessageTimeline } from "../chat/MessageTimeline";
import { ChatInput } from "../input/ChatInput";

const STATUS_LABEL: Record<string, string> = {
	todo: "待办",
	in_progress: "进行中",
	done: "已完成",
};

export function TaskDetailPage() {
	const {
		activeTaskDetailProjectId,
		activeTaskDetailTaskId,
		activeTaskDetailSessionId,
		projects,
		switchView,
		switchProject,
	} = useLayoutStore((s) => s);

	const { setActiveSession, loadConversationMessages, clearMessages } = useChatStore((s) => s);

	const [task, setTask] = useState<ProjectTask | null>(null);

	const project = projects.find((p) => p.id === activeTaskDetailProjectId);

	useEffect(() => {
		if (!activeTaskDetailSessionId) return;

		setActiveSession(activeTaskDetailSessionId);
		loadConversationMessages(activeTaskDetailSessionId);
	}, [activeTaskDetailSessionId, setActiveSession, loadConversationMessages]);

	useEffect(() => {
		if (!activeTaskDetailTaskId) return;

		taskApi.get({ public_id: activeTaskDetailTaskId }).then((res) => {
			const bt = res.data.data;
			if (bt) {
				setTask({
					id: bt.public_id,
					title: bt.title,
					meta: bt.description ?? bt.task_type ?? "",
					status: (bt.status as ProjectTask["status"]) ?? "todo",
					taskType: bt.task_type,
					deadline: bt.deadline,
					description: bt.description,
				});
			}
		}).catch((err) => {
			console.error("TaskDetailPage fetch task error:", err);
		});
	}, [activeTaskDetailTaskId]);

	useEffect(() => {
		return () => {
			clearMessages();
		};
	}, [clearMessages]);

	if (!activeTaskDetailProjectId || !activeTaskDetailTaskId || !activeTaskDetailSessionId) {
		return (
			<div className="flex h-full flex-1 items-center justify-center bg-[var(--leros-app-bg)] text-[var(--leros-text-muted)]">
				无任务详情
			</div>
		);
	}

	return (
		<div data-slot="task-detail-page" className="flex h-full flex-1 flex-col bg-[var(--leros-surface)]">
			<header className="flex h-16 shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10">
				<div className="flex items-center gap-3 text-[var(--leros-text-muted)]">
					{project && (
						<>
						<button
							type="button"
							onClick={() => activeTaskDetailProjectId && switchProject(activeTaskDetailProjectId)}
							className="text-xs font-semibold uppercase tracking-widest hover:text-[var(--leros-text-strong)]"
						>
								{project.name}
							</button>
							<span className="text-[var(--leros-text-subtle)]">/</span>
						</>
					)}
					<h1 className="text-base font-bold text-[var(--leros-text-strong)]">
						{task?.title ?? "任务"}
					</h1>
				</div>
				<div className="flex items-center gap-3">
					<button
						type="button"
						onClick={() => switchView("workbench")}
						className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-semibold text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
					>
						<ArrowLeft className="size-3.5" />
						返回工作台
					</button>
				</div>
			</header>

			{task && (
				<div className="shrink-0 border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-10 py-4">
					<div className="flex flex-wrap items-center gap-4">
						<span
							className={cn(
								"inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold",
								task.status === "done"
									? "bg-[var(--leros-primary-soft)] text-[var(--leros-primary)]"
									: task.status === "in_progress"
										? "bg-[var(--leros-warning)]/10 text-[var(--leros-warning)]"
										: "bg-[var(--leros-chat-control-bg)] text-[var(--leros-text-muted)]",
							)}
						>
							{task.status === "done" ? (
								<CheckCircle2 className="size-3.5" />
							) : task.status === "in_progress" ? (
								<LoaderCircle className="size-3.5" />
							) : (
								<Circle className="size-3.5" />
							)}
							{STATUS_LABEL[task.status] ?? task.status}
						</span>
						{task.taskType && (
							<span className="inline-flex items-center gap-1 rounded-full bg-[var(--leros-primary-softer)] px-2.5 py-0.5 text-xs font-medium text-[var(--leros-primary)]">
								<Tag className="size-3" />
								{task.taskType}
							</span>
						)}
						{task.deadline && (
							<span className="inline-flex items-center gap-1 rounded-full bg-[var(--leros-chat-control-bg)] px-2.5 py-0.5 text-xs font-medium text-[var(--leros-text-muted)]">
								<Calendar className="size-3" />
								{task.deadline}
							</span>
						)}
					</div>
				</div>
			)}

			<div className="min-h-0 flex flex-1">
				<main className="flex min-h-0 flex-1 flex-col">
					<MessageTimeline
						emptyState={<TaskChatEmptyState />}
						contentClassName="max-w-[780px] px-8 py-8 sm:px-8 lg:px-8"
					/>
					<ChatInput variant="project" />
				</main>

				<aside className="flex w-[320px] shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5 py-6">
					<div className="min-h-0 flex-1 space-y-8 overflow-y-auto pr-1">
						{task?.description && (
							<section>
								<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
									任务描述
								</h3>
								<p className="text-sm leading-relaxed text-[var(--leros-text)]">
									{task.description}
								</p>
							</section>
						)}
						{project && (
							<section>
								<h3 className="mb-3 text-xs font-semibold text-[var(--leros-text-muted)]">
									所属项目
								</h3>
								<div className="rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-3.5">
									<p className="text-sm font-semibold text-[var(--leros-text-strong)]">
										{project.name}
									</p>
									{project.description && (
										<p className="mt-1 text-xs text-[var(--leros-text-muted)]">
											{project.description}
										</p>
									)}
								</div>
							</section>
						)}
					</div>
				</aside>
			</div>
		</div>
	);
}

function TaskChatEmptyState() {
	return (
		<div className="flex h-full items-center justify-center px-8">
			<div className="flex max-w-[320px] flex-col items-center text-center">
				<div className="flex size-12 items-center justify-center rounded-full bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
					<Bot className="size-6" />
				</div>
				<h2 className="mt-5 text-lg font-semibold text-[var(--leros-text-strong)]">任务会话</h2>
				<p className="mt-2 text-sm leading-6 text-[var(--leros-text-muted)]">
					在此与 AI 协作完成任务讨论，发送消息即可开始对话。
				</p>
			</div>
		</div>
	);
}
