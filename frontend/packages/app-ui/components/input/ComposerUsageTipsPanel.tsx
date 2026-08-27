"use client";

import { cn } from "@leros/ui/lib/utils";
import { ChevronRight, Lightbulb, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";

type ComposerUsageTipsPanelProps = {
	tips: Array<{ id: string; label: string; prompt: string }>;
	onApply: (prompt: string) => void;
	className?: string;
};

/** Windows ClearType 旋转文字易糊；仅在非 Windows 保留旋转动效。 */
function canUseCrispTextRotate(): boolean {
	if (typeof navigator === "undefined") return false;
	return !/win/i.test(navigator.platform) && !/windows/i.test(navigator.userAgent);
}

export function ComposerUsageTipsPanel({
	tips,
	onApply,
	className,
}: ComposerUsageTipsPanelProps) {
	const [enableRotate, setEnableRotate] = useState(false);

	useEffect(() => {
		setEnableRotate(canUseCrispTextRotate());
	}, []);

	return (
		<div className={cn("mb-4", className)}>
			<div className="mb-3 flex items-center gap-2 text-sm font-medium text-[var(--leros-text-muted)]">
				<Lightbulb className="size-4 shrink-0" />
				<span>使用提示</span>
			</div>
			<div className="flex min-w-0 flex-wrap items-center gap-2.5">
				{tips.map((tip) => (
					<button
						key={tip.id}
						type="button"
						onClick={() => onApply(tip.prompt)}
						className={cn(
							"inline-flex max-w-full items-center gap-2 rounded-full bg-white px-3.5 py-2 text-left shadow-sm ring-1 ring-slate-200/80 transition hover:bg-slate-50",
							enableRotate && "hover:rotate-5",
						)}
					>
						<Sparkles className="size-3.5 shrink-0 text-slate-400" />
						<span className="truncate text-sm text-[var(--leros-text)]">{tip.label}</span>
						<ChevronRight className="size-3.5 shrink-0 text-slate-300" />
					</button>
				))}
			</div>
		</div>
	);
}
