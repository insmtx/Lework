"use client";

import type { ProjectMember } from "@lework/store";
import { Tooltip, TooltipContent, TooltipTrigger } from "@lework/ui/components/ui/tooltip";
import { TriangleAlert } from "lucide-react";

export const PROJECT_MCP_COLLABORATION_WARNING =
	"项目包含多名真人队友，为保护个人连接器数据，任务执行时不会使用 MCP 连接器。";

export function hasMultipleHumanProjectMembers(members: ProjectMember[]): boolean {
	return members.filter((member) => member.type === "user").length >= 2;
}

export function ProjectMCPCollaborationWarning({ members }: { members: ProjectMember[] }) {
	if (!hasMultipleHumanProjectMembers(members)) return null;

	return (
		<Tooltip>
			<TooltipTrigger
				type="button"
				aria-label={PROJECT_MCP_COLLABORATION_WARNING}
				className="inline-flex size-5 items-center justify-center rounded text-amber-500 outline-none transition-colors hover:text-amber-600 focus-visible:ring-2 focus-visible:ring-amber-400/60"
			>
				<TriangleAlert className="size-4" aria-hidden="true" />
			</TooltipTrigger>
			<TooltipContent side="top">{PROJECT_MCP_COLLABORATION_WARNING}</TooltipContent>
		</Tooltip>
	);
}
