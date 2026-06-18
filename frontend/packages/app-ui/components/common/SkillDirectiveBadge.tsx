"use client";

import { cn } from "@leros/ui/lib/utils";
import { Sparkles } from "lucide-react";

export type ParsedSkillDirectives = {
	skills: string[];
	rest: string;
};

export function parseLeadingSkillDirectives(content: string): ParsedSkillDirectives {
	let rest = content.trimStart();
	const skills: string[] = [];

	while (rest.startsWith("/")) {
		const match = rest.match(/^\/([^\s/]+)(?:\s+|$)/);
		if (!match?.[1]) break;
		skills.push(match[1]);
		rest = rest.slice(match[0].length).trimStart();
	}

	return { skills, rest };
}

export function SkillDirectiveBadge({
	name,
	variant = "default",
}: {
	name: string;
	variant?: "default" | "on-blue";
}) {
	return (
		<span
			className={cn(
				"inline-flex max-w-full items-center gap-1.5 rounded-lg px-2 py-1 text-xs font-medium leading-none ring-1",
				variant === "on-blue"
					? "bg-transparent px-0 text-violet-700 ring-0"
					: "bg-violet-50 text-violet-700 ring-violet-100",
			)}
		>
			<span
				className={cn(
					"inline-flex size-4 shrink-0 items-center justify-center rounded-md",
					variant === "on-blue"
						? "bg-violet-500/15 text-violet-700 ring-1 ring-violet-500/20"
						: "bg-white text-violet-600",
				)}
			>
				<Sparkles className="size-3" />
			</span>
			<span className="truncate">/{name}</span>
		</span>
	);
}
