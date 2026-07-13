"use client";

import { cn } from "@leros/ui/lib/utils";
import { ChevronRight, Lightbulb, Sparkles } from "lucide-react";

type ComposerUsageTipsPanelProps = {
	tips: Array<{ id: string; label: string; prompt: string }>;
	onApply: (prompt: string) => void;
	className?: string;
};

export function ComposerUsageTipsPanel({ tips, onApply, className }: ComposerUsageTipsPanelProps) {
	return (
		<div className={cn("mb-4", className)}>
			<div className="mb-3 flex items-center gap-2 text-sm font-medium text-[var(--leros-text-muted)]">
				<Lightbulb className="size-4 shrink-0" />
				<span>使用提示</span>
			</div>
			<div className="flex flex-wrap gap-3">
				{tips.map((tip) => (
					<button
						key={tip.id}
						type="button"
						onClick={() => onApply(tip.prompt)}
						className="inline-flex w-auto max-w-full items-center gap-3 rounded-xl bg-white px-4 py-2 text-left shadow-sm ring-1 ring-slate-200/70 transition-colors hover:bg-slate-50"
					>
						<Sparkles className="size-4 shrink-0 text-violet-500" />
						<span className="whitespace-nowrap text-sm text-[var(--leros-text)]">{tip.label}</span>
						<ChevronRight className="size-4 shrink-0 text-slate-300" />
					</button>
				))}
			</div>
		</div>
	);
}
