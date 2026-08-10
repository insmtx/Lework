"use client";

import type { AutomationExecutionItem, AutomationItem } from "@leros/store";
import { useAutomationStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@leros/ui/components/ui/sheet";
import { cn } from "@leros/ui/lib/utils";
import { Loader2, Pencil, Play, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout";
import { buildFormSummary, buildScheduleFormState } from "./automationForm";

export type AutomationExecutionDrawerProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	automationPublicId: string;
	automationName: string;
	automation?: AutomationItem | null;
	navigation?: AppNavigation;
	onEdit?: (automation: AutomationItem) => void;
	onDelete?: (automation: AutomationItem) => void;
	onRunNow?: (automation: AutomationItem) => void;
};

export function AutomationExecutionDrawer({
	open,
	onOpenChange,
	automationPublicId,
	automationName,
	automation,
	navigation,
	onEdit,
	onDelete,
	onRunNow,
}: AutomationExecutionDrawerProps) {
	const { automations, executions, executionsLoading, executionsError, fetchExecutions } =
		useAutomationStore((s) => s);
	const { isAuthenticated } = useAuth();
	const [selectedExecPublicId, setSelectedExecPublicId] = useState<string | null>(null);

	const currentAutomation =
		automation || automations.find((a) => a.publicId === automationPublicId);

	useEffect(() => {
		if (!open || !isAuthenticated) return;
		setSelectedExecPublicId(null);
		void fetchExecutions(automationPublicId);
	}, [open, isAuthenticated, automationPublicId, fetchExecutions]);

	// 默认选中最新一条执行记录
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
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent
				side="right"
				className="flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-[840px] md:max-w-[900px] border-l border-slate-200 bg-white"
			>
				{/* 顶栏：标题 + 操作工具栏 */}
				<SheetHeader className="flex flex-row items-center justify-between border-b border-slate-100 px-8 py-5 space-y-0">
					<SheetTitle className="text-xl font-bold text-slate-900 tracking-tight">
						{currentAutomation?.name || automationName}
					</SheetTitle>

					<div className="flex items-center gap-2 pr-6">
						{currentAutomation && onEdit ? (
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								className="size-8 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
								title="编辑"
								onClick={() => onEdit(currentAutomation)}
							>
								<Pencil className="size-4" />
							</Button>
						) : null}

						{currentAutomation && onDelete ? (
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								className="size-8 text-slate-500 hover:bg-slate-100 hover:text-red-600"
								title="删除"
								onClick={() => onDelete(currentAutomation)}
							>
								<Trash2 className="size-4" />
							</Button>
						) : null}

						{currentAutomation && onRunNow ? (
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								className="size-8 text-slate-500 hover:bg-indigo-50 hover:text-[#4f46e5]"
								title="立即运行"
								onClick={() => onRunNow(currentAutomation)}
							>
								<Play className="size-4" />
							</Button>
						) : null}

						<Button
							type="button"
							variant="outline"
							size="sm"
							className="ml-2 border-red-200 px-3 text-xs font-medium text-red-500 hover:border-red-300 hover:bg-red-50 hover:text-red-600"
							onClick={() => onOpenChange(false)}
						>
							关闭任务
						</Button>
					</div>
				</SheetHeader>

				{/* 双栏主界面 */}
				<div className="flex min-h-0 flex-1 overflow-hidden">
					{/* 左栏：执行记录 */}
					<div className="flex w-64 sm:w-72 shrink-0 flex-col border-r border-slate-100 p-6 min-h-0">
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
											<span>{formatExecutionTime(exec.scheduledAt || exec.createdAt)}</span>
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
									{currentAutomation?.instruction || "暂无指令"}
								</div>
							</div>
						</div>
					</div>
				</div>
			</SheetContent>
		</Sheet>
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

function formatExecutionTime(value?: string | number): string {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return String(value);
	const pad = (n: number) => String(n).padStart(2, "0");
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
		date.getHours(),
	)}:${pad(date.getMinutes())}`;
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
