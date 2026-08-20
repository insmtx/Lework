"use client";

import { type AutomationItem, skillChipsToPlainText } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Switch } from "@leros/ui/components/ui/switch";
import { cn } from "@leros/ui/lib/utils";
import { Calendar, Clock, MoreHorizontal, Pencil, Play, Trash2 } from "lucide-react";
import { formatLocalDateTime } from "./automationTime";

export type AutomationCardProps = {
	automation: AutomationItem;
	onOpen: (automation: AutomationItem) => void;
	/** 立即运行按钮：Phase 1 暂不开放 */
	onRunNow?: (automation: AutomationItem) => void;
	onEdit: (automation: AutomationItem) => void;
	onDelete: (automation: AutomationItem) => void;
	onToggle: (automation: AutomationItem, enabled: boolean) => void;
	onShowExecutions?: (automation: AutomationItem) => void;
};

export function AutomationCard({
	automation,
	onOpen,
	onRunNow,
	onEdit,
	onDelete,
	onToggle,
}: AutomationCardProps) {
	return (
		<div
			data-slot="automation-card"
			className={cn(
				"group relative flex w-full flex-col justify-between rounded-xl border border-slate-200 bg-white p-4 text-left transition-all duration-200 shadow-2xs hover:border-[#4f46e5]/40 hover:shadow-xs",
				!automation.enabled && "opacity-75 bg-slate-50/50",
			)}
		>
			{/* 主内容交互区 */}
			<button
				type="button"
				className="flex min-w-0 flex-1 cursor-pointer flex-col text-left outline-none"
				onClick={() => onOpen(automation)}
			>
				{/* 头部：图标 + 标题 */}
				<div className="flex w-full min-w-0 items-center gap-2.5 pr-12">
					<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-indigo-50 text-[#4f46e5] transition-colors group-hover:bg-[#4f46e5] group-hover:text-white">
						<Clock className="size-4" />
					</div>
					<h3 className="truncate text-sm font-semibold text-slate-900 transition-colors group-hover:text-[#4f46e5]">
						{automation.name}
					</h3>
				</div>

				{/* 任务指令 */}
				<p className="mt-2.5 line-clamp-2 min-h-9 w-full text-xs leading-relaxed text-slate-500">
					{skillChipsToPlainText(automation.instruction || "") || "暂无指令"}
				</p>
			</button>

			{/* 底部元信息与操作栏 */}
			<div className="mt-3 flex items-center justify-between border-t border-slate-100 pt-2.5 text-xs text-slate-500">
				<div className="min-w-0 flex-1 space-y-0.5">
					<div className="flex items-center gap-1.5 text-slate-700">
						<Calendar className="size-3.5 shrink-0 text-slate-400" />
						<span className="truncate font-medium">
							{automation.summary || automation.scheduleMode}
						</span>
					</div>
					<div className="flex items-center gap-1.5 text-slate-400">
						<Clock className="size-3.5 shrink-0 text-slate-400" />
						<span>
							下次：
							<span className="font-medium text-slate-600">
								{automation.nextRunAt ? formatLocalDateTime(automation.nextRunAt) : "—"}
							</span>
						</span>
					</div>
				</div>

				{/* 操作菜单 */}
				<div className="flex items-center gap-0.5 pl-2">
					{onRunNow ? (
						<Button
							type="button"
							size="icon-xs"
							variant="ghost"
							className="size-7 text-slate-400 hover:bg-indigo-50 hover:text-[#4f46e5]"
							title="立即运行"
							onClick={(e) => {
								e.stopPropagation();
								onRunNow(automation);
							}}
						>
							<Play className="size-3.5" />
						</Button>
					) : null}
					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button
									variant="ghost"
									size="icon-xs"
									className="size-7 shrink-0 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
								>
									<MoreHorizontal className="size-3.5" />
								</Button>
							}
						/>
						<DropdownMenuContent align="end" sideOffset={4}>
							<DropdownMenuItem onClick={() => onEdit(automation)}>
								<Pencil className="mr-2 size-3.5" />
								编辑
							</DropdownMenuItem>
							<DropdownMenuItem variant="destructive" onClick={() => onDelete(automation)}>
								<Trash2 className="mr-2 size-3.5" />
								删除
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			</div>

			{/* 启停开关：与主交互区 button 是兄弟，点击不会冒泡，无需 stopPropagation */}
			<div className="absolute right-4 top-4 z-10 flex items-center">
				<Switch
					checked={automation.enabled}
					onCheckedChange={(checked) => onToggle(automation, checked)}
					aria-label={automation.enabled ? "停用" : "启用"}
				/>
			</div>
		</div>
	);
}
