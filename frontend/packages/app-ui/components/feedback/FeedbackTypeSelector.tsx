"use client";

import type { FeedbackType } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import type { LucideIcon } from "lucide-react";
import { Bug, Lightbulb, MessageSquareMore, Sparkles } from "lucide-react";

const OPTIONS: {
	value: FeedbackType;
	label: string;
	hint: string;
	icon: LucideIcon;
}[] = [
	{
		value: "problem",
		label: "遇到问题",
		hint: "功能异常、Bug、卡住无法继续",
		icon: Bug,
	},
	{
		value: "suggestion",
		label: "功能建议",
		hint: "希望新增或优化某能力",
		icon: Lightbulb,
	},
	{
		value: "experience",
		label: "体验不好",
		hint: "流程不顺、界面难懂、不好用",
		icon: Sparkles,
	},
	{
		value: "other",
		label: "其他",
		hint: "以上均不适用",
		icon: MessageSquareMore,
	},
];

type FeedbackTypeSelectorProps = {
	value: FeedbackType | null;
	onChange: (value: FeedbackType) => void;
};

export function FeedbackTypeSelector({ value, onChange }: FeedbackTypeSelectorProps) {
	return (
		<div className="space-y-2">
			<p className="text-xs font-medium text-slate-500">
				反馈类型 <span className="text-red-500">*</span>
			</p>
			<div className="grid grid-cols-2 gap-2">
				{OPTIONS.map((option) => {
					const Icon = option.icon;
					const selected = value === option.value;
					return (
						<button
							key={option.value}
							type="button"
							onClick={() => onChange(option.value)}
							className={cn(
								"group rounded-lg border px-3 py-2.5 text-left transition-all",
								selected
									? "border-[var(--leros-primary)] bg-[var(--leros-primary-subtle)] shadow-sm ring-1 ring-[var(--leros-primary)]/20"
									: "border-[var(--leros-control-border)] bg-white hover:border-[var(--leros-text-muted)] hover:bg-[var(--leros-surface-soft)]",
							)}
						>
							<span className="flex items-start gap-2">
								<span
									className={cn(
										"mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md transition-colors",
										selected
											? "bg-[var(--leros-primary)] text-white"
											: "bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] group-hover:text-[var(--leros-text-strong)]",
									)}
								>
									<Icon className="size-3.5" />
								</span>
								<span className="min-w-0">
									<span className="block text-sm font-medium text-[var(--leros-text-strong)]">
										{option.label}
									</span>
									<span className="mt-0.5 block text-xs leading-relaxed text-[var(--leros-text-muted)]">
										{option.hint}
									</span>
								</span>
							</span>
						</button>
					);
				})}
			</div>
		</div>
	);
}
