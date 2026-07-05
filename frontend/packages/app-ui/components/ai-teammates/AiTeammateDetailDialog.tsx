"use client";

import type { BackendAITeammateTemplate } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { cn } from "@leros/ui/lib/utils";
import { Eye, Heart, type LucideIcon } from "lucide-react";
import { APP_LOGO_SRC } from "../../assets";
import { MarkdownRenderer } from "../common/MarkdownRenderer";

export type AiTeammateDetailDialogItem = {
	id: number;
	name: string;
	description: string;
	provider: string;
	useCount: number;
	recommendCount: number;
	icon: LucideIcon;
	iconBg: string;
	iconColor: string;
	categoryLabel: string;
	template: BackendAITeammateTemplate;
};

type AiTeammateDetailDialogProps = {
	open: boolean;
	item: AiTeammateDetailDialogItem | null;
	adopting?: boolean;
	adopted?: boolean;
	recommending?: boolean;
	recommended?: boolean;
	onOpenChange: (open: boolean) => void;
	onAdopt: (item: AiTeammateDetailDialogItem) => void;
	onRecommend?: (item: AiTeammateDetailDialogItem) => void;
};

export function AiTeammateDetailDialog({
	open,
	item,
	adopting = false,
	adopted = false,
	recommending = false,
	recommended = false,
	onOpenChange,
	onAdopt,
	onRecommend,
}: AiTeammateDetailDialogProps) {
	if (!item) {
		return <Dialog open={open} onOpenChange={onOpenChange} />;
	}

	const Icon = item.icon;
	const featureKeywords = buildFeatureKeywords(item);
	const expertise = buildExpertise(item);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[min(92dvh,760px)] max-w-[min(92vw,760px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogTitle className="sr-only">{item.name}</DialogTitle>
				<DialogDescription className="sr-only">{item.description}</DialogDescription>

				<div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6 pt-7 sm:px-8">
					<div className="flex items-start gap-4 pr-8">
						<div
							className={cn(
								"flex size-20 shrink-0 items-center justify-center rounded-2xl sm:size-24",
								item.iconBg,
								item.iconColor,
							)}
						>
							<Icon className="size-10 sm:size-12" aria-hidden="true" />
						</div>

						<div className="min-w-0 flex-1">
							<h2 className="truncate text-xl font-semibold text-[var(--leros-text-strong)] sm:text-2xl">
								{item.name}
							</h2>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								<span className="inline-flex items-center gap-1.5 rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]">
									<img src={APP_LOGO_SRC} alt="" className="size-3.5 rounded object-cover" />
									{item.provider}
								</span>
								{featureKeywords.map((keyword) => (
									<span
										key={keyword}
										className="rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]"
									>
										{keyword}
									</span>
								))}
							</div>
							<div className="mt-4 flex items-center gap-4 text-xs text-[var(--leros-text-subtle)]">
								<span className="inline-flex items-center gap-1.5">
									<Eye className="size-3.5" aria-hidden="true" />
									{formatCompactCount(item.useCount)}次使用
								</span>
								<button
									type="button"
									className={cn(
										"inline-flex items-center gap-1.5 rounded px-1 py-0.5 transition-colors hover:text-rose-500 disabled:cursor-not-allowed disabled:opacity-70",
										recommended && "text-rose-500",
									)}
									onClick={() => onRecommend?.(item)}
									disabled={recommending || recommended}
									aria-label={recommended ? "已点赞" : `点赞 ${item.name}`}
									title={recommended ? "已点赞" : "点赞"}
								>
									<Heart
										className={cn("size-3.5", recommended && "fill-current")}
										aria-hidden="true"
									/>
									{formatCompactCount(item.recommendCount)}次推荐
								</button>
							</div>
						</div>
					</div>

					<div className="mt-8 space-y-6">
						<section>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">能力介绍</h3>
							<MarkdownRenderer
								content={item.description || "暂无能力介绍"}
								className="prose prose-slate prose-sm mt-3 max-w-2xl prose-p:my-1.5 prose-p:leading-7 prose-p:text-[var(--leros-text-muted)]"
							/>
						</section>

						<section>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">擅长领域</h3>
							<div className="mt-3 flex flex-wrap gap-3">
								{expertise.map((tag) => (
									<span
										key={tag}
										className="rounded-md border border-[var(--leros-control-border)] bg-white px-4 py-2 text-sm font-medium text-[var(--leros-text-muted)]"
									>
										{tag}
									</span>
								))}
							</div>
						</section>
					</div>
				</div>

				<DialogFooter className="border-t border-[var(--leros-control-border)] bg-white px-6 py-4 sm:px-8">
					<Button
						type="button"
						className="h-11 w-full rounded-lg bg-[var(--leros-text-strong)] text-sm font-semibold text-white hover:bg-[var(--leros-text)]"
						onClick={() => onAdopt(item)}
						disabled={adopting || adopted}
					>
						{adopted ? "已添加" : adopting ? "添加中" : "添加到我的队友"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function buildFeatureKeywords(item: AiTeammateDetailDialogItem): string[] {
	const source = [item.categoryLabel, ...(item.template.tags ?? [])];
	return uniqueNonEmpty(source).slice(0, 3);
}

function buildExpertise(item: AiTeammateDetailDialogItem): string[] {
	const expertise = uniqueNonEmpty(item.template.expertise ?? []);
	if (expertise.length > 0) return expertise;
	return buildFeatureKeywords(item);
}

function uniqueNonEmpty(values: Array<string | undefined>): string[] {
	const seen = new Set<string>();
	const result: string[] = [];

	for (const value of values) {
		const normalized = value?.trim();
		if (!normalized || seen.has(normalized)) continue;
		seen.add(normalized);
		result.push(normalized);
	}

	return result;
}

function formatCompactCount(value: number): string {
	if (value >= 10000) return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)}万`;
	if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}千`;
	return String(value);
}
