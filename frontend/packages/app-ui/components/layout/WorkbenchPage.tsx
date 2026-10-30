"use client";

import { useLayoutStore } from "@leros/store";
import { getRequestErrorMessage } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { BidComparisonIcon } from "../../assets";
import { useAuth } from "../auth";
import {
	type BidComparisonConfig,
	BidComparisonConfigDialog,
} from "../input/BidComparisonConfigDialog";
import {
	bidComparisonConfigToAttachments,
	bidComparisonOutputFormat,
	bidComparisonPrompt,
	ensureBidComparisonFilesUploaded,
} from "../input/bidComparisonAttachments";
import type { AppNavigation } from "./LeftRail";

export function WorkbenchPage({ navigation }: { navigation?: AppNavigation }) {
	const { projects, fetchProjects, fetchTasks, sendWorkbenchMessage } = useLayoutStore((s) => s);
	const { requireAuth } = useAuth();
	const [bidComparisonOpen, setBidComparisonOpen] = useState(false);

	const projectOptions = useMemo(
		() => projects.map((project) => ({ id: project.id, name: project.name, tasks: project.tasks })),
		[projects],
	);

	const openBidComparison = () => {
		requireAuth(() => {
			void fetchProjects();
			setBidComparisonOpen(true);
		});
	};

	const startBidComparison = async (config: BidComparisonConfig) => {
		try {
			const resolved = await ensureBidComparisonFilesUploaded(config, config.projectId);
			const result = await sendWorkbenchMessage(
				bidComparisonPrompt(resolved),
				resolved.projectId,
				"default",
				bidComparisonConfigToAttachments(resolved),
				undefined,
				undefined,
				undefined,
				"bid_comparison",
				bidComparisonOutputFormat(resolved),
				resolved.taskId ?? null,
			);
			if (!result?.project_id || !result.task_id || !result.session_id) {
				throw new Error("启动标书对比失败，请确认所选任务未在回复中后重试");
			}
			navigation?.goToTaskDetail(result.project_id, result.task_id, result.session_id);
		} catch (error) {
			console.error("WorkbenchPage start bid comparison error:", error);
			toast.error(getRequestErrorMessage(error) ?? "启动标书对比失败");
			throw error;
		}
	};

	return (
		<div className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]">
			<header className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-5">
				<h1 className="text-xl font-semibold text-[var(--leros-text-strong)]">工作台</h1>
				<p className="mt-2 text-sm text-[var(--leros-text-muted)]">
					选择业务功能，快速开始处理工作资料。
				</p>
			</header>
			<main className="flex min-h-0 flex-1 flex-col px-6 py-6">
				<div className="w-full">
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
						<button
							type="button"
							onClick={openBidComparison}
							className={cn(
								"group flex min-h-[132px] cursor-pointer flex-col rounded-lg border border-slate-200 bg-white p-4",
								"text-left transition-colors hover:border-[var(--leros-primary-soft)]",
								"hover:bg-[var(--leros-primary-softer)]/35",
							)}
						>
							<div className="flex size-10 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								<BidComparisonIcon className="size-5" />
							</div>
							<h3 className="mt-4 text-sm font-semibold text-[var(--leros-text-strong)]">
								标书对比
							</h3>
							<p className="mt-1 text-xs leading-5 text-[var(--leros-text-muted)]">
								选择投标文件与对比文件，发起标书对比分析任务。
							</p>
						</button>
					</div>
				</div>
			</main>
			<BidComparisonConfigDialog
				open={bidComparisonOpen}
				onOpenChange={setBidComparisonOpen}
				onSave={startBidComparison}
				onProjectChange={fetchTasks}
				projects={projectOptions}
			/>
		</div>
	);
}
