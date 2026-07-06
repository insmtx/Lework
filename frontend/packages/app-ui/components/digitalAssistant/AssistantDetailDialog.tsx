"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { ChevronRight } from "lucide-react";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { AssistantAvatar } from "./AssistantAvatar";
import { buildFeatureKeywords, buildPromptSuggestions, uniqueNonEmpty } from "./promptSuggestions";

type AssistantDetailDialogProps = {
	assistant: DigitalAssistantItem | null;
	open: boolean;
	summoning?: boolean;
	onOpenChange: (open: boolean) => void;
	onSummon: (assistant: DigitalAssistantItem, prompt?: string) => void;
};

export function AssistantDetailDialog({
	assistant,
	open,
	summoning = false,
	onOpenChange,
	onSummon,
}: AssistantDetailDialogProps) {
	if (!assistant) {
		return <Dialog open={open} onOpenChange={onOpenChange} />;
	}

	const featureKeywords = buildFeatureKeywords(assistant);
	const expertise = uniqueNonEmpty(assistant.expertise);
	const promptSuggestions = buildPromptSuggestions(assistant);
	const buttonState = resolveSummonButtonState(assistant);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[min(88dvh,640px)] max-w-[min(92vw,520px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogTitle className="sr-only">{assistant.name}</DialogTitle>
				<DialogDescription className="sr-only">查看 AI 队友详情</DialogDescription>

				<div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5 pt-6 sm:px-6">
					<div className="flex items-start gap-4 pr-7">
						<AssistantAvatar name={assistant.name} src={assistant.avatar} size="lg" />
						<div className="min-w-0 flex-1">
							<h2 className="truncate text-xl font-semibold text-[var(--leros-text-strong)]">
								{assistant.name}
							</h2>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								{featureKeywords.map((keyword) => (
									<span
										key={keyword}
										className="rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-1 text-xs font-medium text-[var(--leros-text-muted)]"
									>
										{keyword}
									</span>
								))}
							</div>
						</div>
					</div>

					<div className="mt-6 space-y-5">
						<section>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">能力介绍</h3>
							<MarkdownRenderer
								content={assistant.description || "暂无能力介绍"}
								className="prose prose-slate prose-sm mt-3 max-w-2xl prose-p:my-1.5 prose-p:leading-7 prose-p:text-[var(--leros-text-muted)]"
							/>
						</section>

						<section>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">擅长领域</h3>
							<div className="mt-3 flex flex-wrap gap-3">
								{expertise.length > 0 ? (
									expertise.map((tag) => (
										<span
											key={tag}
											className="rounded-md border border-[var(--leros-control-border)] bg-white px-4 py-2 text-sm font-medium text-[var(--leros-text-muted)]"
										>
											{tag}
										</span>
									))
								) : (
									<span className="text-sm text-[var(--leros-text-subtle)]">暂无</span>
								)}
							</div>
						</section>

						<section>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">
								试试这样问我
							</h3>
							<div className="mt-3 space-y-3">
								{promptSuggestions.map((prompt) => (
									<button
										key={prompt}
										type="button"
										className="flex w-full items-center justify-between gap-3 rounded-lg border border-[var(--leros-control-border)] bg-white px-4 py-3 text-left text-sm leading-6 text-[var(--leros-text-muted)] transition-colors hover:border-[var(--leros-primary)] hover:text-[var(--leros-text-strong)] disabled:cursor-not-allowed disabled:opacity-60"
										onClick={() => onSummon(assistant, prompt)}
										disabled={summoning || buttonState.disabled}
									>
										<span className="min-w-0 flex-1">“{prompt}”</span>
										<ChevronRight
											className="size-4 shrink-0 text-[var(--leros-text-subtle)]"
											aria-hidden="true"
										/>
									</button>
								))}
							</div>
						</section>
					</div>
				</div>

				<DialogFooter className="border-t border-[var(--leros-control-border)] bg-white px-5 py-3.5 sm:px-6">
					<Button
						type="button"
						className="h-10 w-full rounded-lg bg-[var(--leros-text-strong)] text-sm font-semibold text-white hover:bg-[var(--leros-text)] disabled:bg-[var(--leros-text-subtle)]"
						onClick={() => onSummon(assistant)}
						disabled={summoning || buttonState.disabled}
					>
						{summoning ? "召唤中" : buttonState.label}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

type SummonButtonState = { disabled: boolean; label: string };

function resolveSummonButtonState(assistant: DigitalAssistantItem): SummonButtonState {
	if (assistant.status !== "active") {
		if (assistant.status === "inactive") {
			return { disabled: true, label: "已停用·请先启用" };
		}
		return {
			disabled: true,
			label: assistant.status === "draft" ? "草稿·暂不可召唤" : "暂不可召唤",
		};
	}

	const deploymentStatus = assistant.deploymentStatus?.trim();
	if (deploymentStatus === "pending") return { disabled: true, label: "初始化中…" };
	if (deploymentStatus === "provisioning") return { disabled: true, label: "部署中…" };
	if (deploymentStatus === "failed") return { disabled: true, label: "部署失败" };

	return { disabled: false, label: `召唤 ${assistant.name}` };
}
