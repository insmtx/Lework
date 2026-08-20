"use client";

import { cn } from "@leros/ui/lib/utils";
import { Sparkles } from "lucide-react";

export function SkillDirectiveBadge({
	name,
	className,
}: {
	name: string;
	className?: string;
}) {
	return (
		<span
			className={cn(
				"inline-flex max-w-full items-center gap-1 rounded-lg bg-violet-50 px-1.5 py-0.5 text-xs font-medium leading-5 text-violet-700 ring-1 ring-violet-100 align-baseline",
				className,
			)}
		>
			<span className="inline-flex size-4 shrink-0 items-center justify-center text-violet-600">
				<Sparkles className="size-3.5" />
			</span>
			<span className="truncate">{name}</span>
		</span>
	);
}
