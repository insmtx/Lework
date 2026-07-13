"use client";

import { Action, type Project, useProjectCapabilities } from "@leros/store";
import { DropdownMenuItem } from "@leros/ui/components/ui/dropdown-menu";
import { LogOut, Pencil, Trash2 } from "lucide-react";
import type { MouseEvent, PointerEvent } from "react";
import { CanGate } from "../permission/CanGate";

function runMenuAction(event: MouseEvent, action: () => void) {
	// 中文注释：菜单关闭时 pointerup 可能落到下方可点击卡片上，preventDefault 阻断这次穿透点击。
	event.preventDefault();
	event.stopPropagation();
	action();
}

function preventClickThrough(event: PointerEvent) {
	event.preventDefault();
	event.stopPropagation();
}

/** 阻断下拉菜单操作穿透到侧栏行点击。 */
export function runRailMenuAction(event: MouseEvent, action: () => void) {
	runMenuAction(event, action);
}

/** 阻断下拉菜单 pointer 事件穿透。 */
export function preventRailMenuClickThrough(event: PointerEvent) {
	preventClickThrough(event);
}

type ProjectActionsMenuProps = {
	project: Project;
	onRename: (project: Project) => void;
	onDelete: (project: Project) => void;
	onLeave: (project: Project) => void;
};

/** 项目更多操作菜单项（重命名 / 删除 / 离开），按权限显隐。 */
export function ProjectActionsMenu({
	project,
	onRename,
	onDelete,
	onLeave,
}: ProjectActionsMenuProps) {
	useProjectCapabilities(project.id);
	const resource = { type: "project" as const, publicId: project.id };

	return (
		<>
			<CanGate action={Action.ProjectUpdate} resource={resource}>
				<DropdownMenuItem
					onPointerDown={preventClickThrough}
					onClick={(event) => runMenuAction(event, () => onRename(project))}
				>
					<Pencil className="size-3.5" />
					<span>重命名</span>
				</DropdownMenuItem>
			</CanGate>
			<CanGate action={Action.ProjectDelete} resource={resource}>
				<DropdownMenuItem
					variant="destructive"
					onPointerDown={preventClickThrough}
					onClick={(event) => runMenuAction(event, () => onDelete(project))}
				>
					<Trash2 className="size-3.5" />
					<span>删除</span>
				</DropdownMenuItem>
			</CanGate>
			<CanGate action={Action.ProjectMemberLeave} resource={resource}>
				<DropdownMenuItem
					onPointerDown={preventClickThrough}
					onClick={(event) => runMenuAction(event, () => onLeave(project))}
				>
					<LogOut className="size-3.5" />
					<span>离开项目</span>
				</DropdownMenuItem>
			</CanGate>
		</>
	);
}
