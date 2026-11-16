"use client";

import type { SubagentView, ToolBlock } from "@lework/tool-view";
import { Button } from "@lework/ui/components/ui/button";
import { X } from "lucide-react";
import { useEffect } from "react";
import { RawJson } from "./raw-json";
import { ToolBlocks } from "./tool-blocks";

/**
 * SubagentDialog 用全屏弹层承载子代理详情。
 *
 * prompt 与结果往往是大段文本，内联展开会把会话流撑得无法阅读；弹层可滚动、
 * 可复制，关闭后回到原来的滚动位置。
 */
export function SubagentDialog({
	open,
	detail,
	blocks,
	raw,
	onClose,
}: {
	open: boolean;
	detail: SubagentView;
	blocks: ToolBlock[];
	raw: string;
	onClose: () => void;
}) {
	useEffect(() => {
		if (!open) return;
		const handleKey = (event: KeyboardEvent) => {
			if (event.key === "Escape") onClose();
		};
		window.addEventListener("keydown", handleKey);
		return () => window.removeEventListener("keydown", handleKey);
	}, [open, onClose]);

	if (!open) return null;
	const meta = [detail.agent, detail.duration ?? "", detail.background ? "background" : ""]
		.filter((part) => part !== "")
		.join(" · ");

	return (
		<div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-6">
			<button
				type="button"
				aria-label="Close subagent detail"
				onClick={onClose}
				className="absolute inset-0 cursor-default bg-black/40"
			/>
			<div
				role="dialog"
				aria-modal="true"
				aria-label="Subagent detail"
				className="relative flex h-[92dvh] w-full max-w-3xl flex-col overflow-hidden rounded-t-2xl border bg-background shadow-xl sm:h-[85vh] sm:rounded-2xl"
			>
				<header className="flex items-center gap-3 border-b px-4 py-3">
					<div className="min-w-0 flex-1">
						<h2 className="text-sm font-semibold">Subagent</h2>
						<p className="truncate text-xs text-muted-foreground">
							{detail.description || detail.agent || "agent"}
						</p>
					</div>
					<Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
						<X className="size-4" />
					</Button>
				</header>
				<div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-4">
					<div className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
						{meta}
					</div>
					{detail.sessionID ? (
						<div className="text-[11px] text-muted-foreground">
							Child session{" "}
							<span className="select-all font-mono text-foreground">{detail.sessionID}</span>
						</div>
					) : null}
					<ToolBlocks blocks={blocks} />
					<RawJson text={raw} />
				</div>
			</div>
		</div>
	);
}
