"use client";

import type { Project } from "@leros/store";
import { useProjectMenuCapabilities } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { cn } from "@leros/ui/lib/utils";
import { MoreHorizontal } from "lucide-react";
import type { MouseEvent } from "react";
import { ProjectActionsMenu } from "./ProjectActionsMenu";

type ProjectActionsDropdownProps = {
	project: Project;
	onRename: (project: Project) => void;
	onDelete: (project: Project) => void;
	onLeave: (project: Project) => void;
	variant: "rail" | "card";
	className?: string;
	slotClassName?: string;
	onOpenChange?: (open: boolean) => void;
};

/** 项目更多操作：无权限时隐藏触发按钮，菜单项与侧栏/项目页统一。 */
export function ProjectActionsDropdown({
	project,
	onRename,
	onDelete,
	onLeave,
	variant,
	className,
	slotClassName,
	onOpenChange,
}: ProjectActionsDropdownProps) {
	const { loading, hasAny } = useProjectMenuCapabilities(project.id);

	if (loading || !hasAny) {
		return null;
	}

	const stopPropagation = (event: MouseEvent) => {
		event.stopPropagation();
	};

	const trigger =
		variant === "rail" ? (
			<button
				type="button"
				aria-label={`管理项目 ${project.name}`}
				className={cn(
					"flex size-6 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-subtle)] transition-[background-color,color] duration-150 hover:bg-black/5 hover:text-[var(--leros-text-strong)]",
					className,
				)}
				onClick={stopPropagation}
			>
				<MoreHorizontal className="size-4" />
			</button>
		) : (
			<Button
				variant="ghost"
				size="icon-xs"
				className={cn(
					"pointer-events-none absolute right-3 top-3 z-10 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100 aria-expanded:pointer-events-auto aria-expanded:opacity-100",
					className,
				)}
				onClick={stopPropagation}
				aria-label={`管理项目 ${project.name}`}
			>
				<MoreHorizontal className="size-3.5" />
			</Button>
		);

	const menu = (
		<DropdownMenu
			onOpenChange={(open) => {
				if (!open) {
					requestAnimationFrame(() => {
						(document.activeElement as HTMLElement | null)?.blur();
					});
				}
				onOpenChange?.(open);
			}}
		>
			<DropdownMenuTrigger render={trigger} />
			<DropdownMenuContent align="end" sideOffset={4}>
				<ProjectActionsMenu
					project={project}
					onRename={onRename}
					onDelete={onDelete}
					onLeave={onLeave}
				/>
			</DropdownMenuContent>
		</DropdownMenu>
	);

	if (variant === "rail" && slotClassName) {
		return (
			<div className={slotClassName} data-rail-menu-slot="">
				{menu}
			</div>
		);
	}

	return menu;
}
