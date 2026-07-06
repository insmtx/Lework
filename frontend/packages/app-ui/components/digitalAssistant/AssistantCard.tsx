"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { useDAStore } from "@leros/store";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Switch } from "@leros/ui/components/ui/switch";
import { cn } from "@leros/ui/lib/utils";
import { MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { AssistantAvatar } from "./AssistantAvatar";
import { getAssistantDisplayStatus } from "./assistantStatus";

export type AssistantCardProps = {
	assistant: DigitalAssistantItem;
	onSelect: (assistant: DigitalAssistantItem) => void;
	onEdit: (assistant: DigitalAssistantItem) => void;
	onDelete: (assistant: DigitalAssistantItem) => void;
};

export function AssistantCard({ assistant, onSelect, onEdit, onDelete }: AssistantCardProps) {
	const { updateAssistantStatus } = useDAStore((s) => s);
	const statusInfo = getAssistantDisplayStatus(assistant);

	const handleToggleStatus = (checked: boolean) => {
		updateAssistantStatus(assistant.id, checked ? "active" : "inactive");
	};

	return (
		<div
			data-slot="assistant-card"
			className={cn(
				"group relative flex gap-4 rounded-lg border p-4 transition-colors w-full text-left",
				"border-slate-200 bg-white hover:border-blue-200 hover:bg-blue-50/30",
			)}
		>
			<button
				type="button"
				className="flex min-w-0 flex-1 cursor-pointer gap-4 text-left outline-none"
				onClick={() => onSelect(assistant)}
			>
				<AssistantAvatar name={assistant.name} src={assistant.avatar} />

				<div className="flex flex-1 flex-col gap-1 min-w-0">
					<div className="flex items-center gap-2">
						<span className="text-sm font-medium text-slate-900 truncate">{assistant.name}</span>
						<Badge
							variant="outline"
							className={cn("text-xs shrink-0", statusInfo.className)}
							title={statusInfo.title}
						>
							{statusInfo.label}
						</Badge>
					</div>
					<span className="text-xs text-slate-500 line-clamp-2">
						{assistant.description || "暂无描述"}
					</span>
					<div className="flex items-center gap-2 mt-1">
						<span className="text-xs text-slate-400">
							更新于 {new Date(assistant.updatedAt).toLocaleDateString("zh-CN")}
						</span>
					</div>
				</div>
			</button>

			<div className="flex flex-col items-center gap-2 shrink-0">
				<DropdownMenu>
					<DropdownMenuTrigger
						render={
							<Button
								variant="ghost"
								size="icon-xs"
								className="opacity-0 group-hover:opacity-100 transition-opacity text-slate-400 hover:text-slate-600 shrink-0"
							>
								<MoreHorizontal className="size-3.5" />
							</Button>
						}
					/>
					<DropdownMenuContent align="end" sideOffset={4}>
						{assistant.source !== "template" && (
							<DropdownMenuItem
								onClick={() => {
									onEdit(assistant);
								}}
							>
								<Pencil className="size-3.5 mr-2" />
								编辑
							</DropdownMenuItem>
						)}
						<DropdownMenuItem
							variant="destructive"
							onClick={() => {
								onDelete(assistant);
							}}
						>
							<Trash2 className="size-3.5 mr-2" />
							删除
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
				<Switch
					size="sm"
					checked={assistant.status === "active"}
					onCheckedChange={handleToggleStatus}
				/>
			</div>
		</div>
	);
}
