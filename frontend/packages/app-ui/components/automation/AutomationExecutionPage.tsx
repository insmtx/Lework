"use client";

import type { AutomationExecutionItem, AutomationItem } from "@leros/store";
import { skillChipsToPlainText, useAutomationStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Switch } from "@leros/ui/components/ui/switch";
import { cn } from "@leros/ui/lib/utils";
import { ArrowLeft, Loader2, Pencil, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout";
import { AutomationDeleteDialog } from "./AutomationDeleteDialog";
import { AutomationFormDialog } from "./AutomationFormDialog";
import { buildFormSummary, buildScheduleFormState } from "./automationForm";
import { formatLocalDateTime } from "./automationTime";

export type AutomationExecutionPageProps = {
	automationPublicId: string;
	navigation?: AppNavigation;
};

// 转成一个独立的自动化任务详情页，左上角有返回按钮。
export function AutomationExecutionPage({
	automationPublicId,
	navigation,
}: AutomationExecutionPageProps) {
	const {
		automations,
		executions,
		executionsLoading,
		executionsError,
		fetchExecutions,
		toggleAutomation,
	} = useAutomationStore((s) => s);
	const { isAuthenticated } = useAuth();
	const [selectedExecPublicId, setSelectedExecPublicId] = useState<string | null>(null);
	const [editTarget, setEditTarget] = useState<AutomationItem | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<AutomationItem | null>(null);

	const currentAutomation = automations.find((a) => a.publicId === automationPublicId) ?? null;

	useEffect(() => {
		if (!isAuthenticated) return;
		setSelectedExecPublicId(null);
		void fetchExecutions(automationPublicId);
	}, [isAuthenticated, automationPublicId, fetchExecutions]);

	useEffect(() => {
		if (executions.length > 0 && !selectedExecPublicId) {
			const first = executions[0];
			if (first) {
				setSelectedExecPublicId(first.publicId);
			}
		}
	}, [executions, selectedExecPublicId]);

	const selectedExec =
		executions.find((e) => e.publicId === selectedExecPublicId) ?? executions[0] ?? null;

	const handleOpenTask = (exec: AutomationExecutionItem) => {
		if (navigation && exec.projectPublicId && exec.taskPublicId && exec.sessionPublicId) {
			navigation.goToTaskDetail(exec.projectPublicId, exec.taskPublicId, exec.sessionPublicId);
		}
	};

	const scheduleText = getScheduleText(currentAutomation);

	return (
		<div className="flex h-full min-h-0 w-full flex-col bg-white">
			{/* 顶栏：返回 + 标题 + 操作 */}
			<header className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
				<div className="flex items-center gap-3">
					<Button
						type="button"
						variant="ghost"
						size="icon-sm"
						className="size-8 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
						title="返回自动化列表"
						onClick={() => navigation?.goToRoute("automation")}
					>
						<ArrowLeft className="size-4" />
					</Button>
					<h2 className="text-lg font-semibold text-slate-900">
						{currentAutomation?.name || "自动化详情"}
					</h2>
				</div>

				{currentAutomation ? (
					<div className="flex items-center gap-3">
						{/* 任务开启状态开关 */}
						<div className="flex items-center gap-2">
							<Switch
								checked={currentAutomation.enabled}
								onCheckedChange={(checked) => {
									void toggleAutomation(currentAutomation.publicId, checked);
								}}
								aria-label={currentAutomation.enabled ? "停用" : "启用"}
							/>
							<span className="text-xs font-medium text-slate-600">
								{currentAutomation.enabled ? "已启用" : "已停用"}
							</span>
						</div>
						<Button
							type="button"
							variant="ghost"
							size="icon-sm"
							className="size-8 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
							title="编辑"
							onClick={() => {
								if (currentAutomation) setEditTarget(currentAutomation);
							}}
						>
							<Pencil className="size-4" />
						</Button>
						<Button
							type="button"
							variant="ghost"
							size="icon-sm"
							className="size-8 text-slate-500 hover:bg-slate-100 hover:text-red-600"
							title="删除"
							onClick={() => {
								if (currentAutomation) setDeleteTarget(currentAutomation);
							}}
						>
							<Trash2 className="size-4" />
						</Button>
					</div>
				) : null}
			</header>

			{/* 双栏主界面 */}
			<div className="flex min-h-0 flex-1 overflow-hidden">
				{/* 左栏：执行记录 */}
				<div className="flex w-64 sm:w-72 shrink-0 flex-col border-r border-slate-200 p-6 min-h-0">
					<h3 className="text-sm font-semibold text-slate-900 mb-4">执行记录</h3>

					<div className="flex-1 overflow-y-auto pr-1 space-y-1">
						{executionsLoading ? (
							<div className="flex h-32 items-center justify-center text-xs text-slate-400">
								<Loader2 className="mr-2 size-3.5 animate-spin" />
								加载中…
							</div>
						) : executionsError ? (
							<div className="flex h-32 items-center justify-center text-xs text-red-500 px-2 text-center">
								{executionsError}
							</div>
						) : executions.length === 0 ? (
							<div className="flex h-32 items-center justify-center text-xs text-slate-400">
								暂无执行记录
							</div>
						) : (
							executions.map((exec) => {
								const isSelected = selectedExec?.publicId === exec.publicId;
								return (
									<button
										key={exec.publicId}
										type="button"
										onClick={() => {
											setSelectedExecPublicId(exec.publicId);
											// 点击执行记录跳到对应任务详情页（有任务链接时）
											handleOpenTask(exec);
										}}
										className={cn(
											"flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-xs text-left transition-colors",
											isSelected
												? "bg-slate-100/80 font-medium text-slate-900"
												: "text-slate-600 hover:bg-slate-50",
										)}
									>
										<span>{formatLocalDateTime(exec.scheduledAt || exec.createdAt)}</span>
										<span
											className={cn(
												"size-2 rounded-full shrink-0 ml-2",
												statusDotClass(exec.status),
											)}
											title={statusLabel(exec.status)}
										/>
									</button>
								);
							})
						)}
					</div>
				</div>

				{/* 右栏：任务详情 */}
				<div className="flex-1 overflow-y-auto p-6 space-y-6">
					<h3 className="text-sm font-semibold text-slate-900">任务详情</h3>

					<div className="space-y-6">
						{/* 执行周期 */}
						<div>
							<div className="text-xs font-medium text-slate-400 mb-1.5">执行周期</div>
							<div className="text-sm text-slate-900 font-medium">{scheduleText}</div>
						</div>

						{/* 任务指令 */}
						<div>
							<div className="text-xs font-medium text-slate-400 mb-1.5">任务指令</div>
							<div className="text-xs leading-relaxed text-slate-700 whitespace-pre-wrap rounded-lg bg-slate-50/70 p-3.5 border border-slate-100/80 font-normal">
								{skillChipsToPlainText(currentAutomation?.instruction || "") || "暂无指令"}
							</div>
						</div>
					</div>
				</div>
			</div>

			<AutomationFormDialog
				open={!!editTarget}
				onOpenChange={(open) => {
					if (!open) setEditTarget(null);
				}}
				editTarget={editTarget}
			/>
			<AutomationDeleteDialog
				open={!!deleteTarget}
				onOpenChange={(open) => {
					if (!open) setDeleteTarget(null);
				}}
				target={deleteTarget}
			/>
		</div>
	);
}

function getScheduleText(automation?: AutomationItem | null): string {
	if (!automation) return "周期触发";
	if (automation.summary) return automation.summary;
	if (automation.formConfig) {
		const summary = buildFormSummary(buildScheduleFormState(automation.formConfig));
		if (summary) return summary;
	}
	return automation.scheduleMode || "周期触发";
}

function statusLabel(status: string): string {
	switch (status) {
		case "queued":
			return "等待执行";
		case "running":
			return "执行中";
		case "succeeded":
			return "执行成功";
		case "failed":
			return "执行失败";
		case "skipped":
			return "已跳过";
		default:
			return status;
	}
}

function statusDotClass(status: string): string {
	switch (status) {
		case "succeeded":
			return "bg-emerald-500";
		case "running":
			return "bg-blue-500 animate-pulse";
		case "queued":
			return "bg-blue-400";
		case "failed":
			return "bg-red-500";
		case "skipped":
			return "bg-slate-300";
		default:
			return "bg-slate-400";
	}
}
