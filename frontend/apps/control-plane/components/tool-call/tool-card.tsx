"use client";

import { toolView } from "@lework/tool-view";
import { cn } from "@lework/ui/lib/utils";
import { useMemo, useState } from "react";
import type { ContentPart } from "../../lib/types";
import { RawJson } from "./raw-json";
import { SubagentDialog } from "./subagent-dialog";
import { ToolBlocks } from "./tool-blocks";

/**
 * ToolCard 把工具调用渲染成可折叠卡片。
 *
 * 通常展开后原地显示块内容；子代理改用弹层，因为它的 prompt 和结果都是长文，
 * 挤在会话流里会盖住上下文。
 */
export function ToolCard({ part }: { part: ContentPart }) {
	const view = useMemo(() => toolView(part), [part]);
	const [expanded, setExpanded] = useState(false);
	const [dialogOpen, setDialogOpen] = useState(false);

	if (!view) return null;

	const detail = view.subagent;
	const expandable = detail ? true : view.blocks.length > 0 || view.raw !== "";
	const toneClass =
		view.tone === "error"
			? "text-destructive"
			: view.tone === "ok"
				? "text-emerald-600 dark:text-emerald-400"
				: "text-muted-foreground";

	const toggle = () => {
		if (!expandable) return;
		if (detail) {
			setDialogOpen(true);
			return;
		}
		setExpanded((value) => !value);
	};

	return (
		<>
			<div className="rounded-lg border bg-background/80 text-xs shadow-sm">
				<button
					type="button"
					onClick={toggle}
					disabled={!expandable}
					aria-label={detail ? "Open subagent detail" : "Toggle tool call details"}
					className="flex w-full items-center gap-2 px-3 py-2 text-left disabled:cursor-default"
				>
					<span className="w-3 shrink-0 font-mono text-muted-foreground">
						{expandable ? (detail ? "›" : expanded ? "▾" : "▸") : "•"}
					</span>
					<span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
						tool
					</span>
					<span className="truncate font-mono font-medium">{view.tool}</span>
					<span className={cn("ml-auto shrink-0 text-[10px]", toneClass)}>{view.status}</span>
				</button>

				{(detail || !expanded) && view.summary !== "" ? (
					<div className="truncate px-3 pb-2 text-[11px] text-muted-foreground">{view.summary}</div>
				) : null}

				{!detail && expanded ? (
					<div className="space-y-2 border-t px-3 py-2">
						<ToolBlocks blocks={view.blocks} />
						<RawJson text={view.raw} />
					</div>
				) : null}
			</div>

			{detail ? (
				<SubagentDialog
					open={dialogOpen}
					detail={detail}
					blocks={view.blocks}
					raw={view.raw}
					onClose={() => setDialogOpen(false)}
				/>
			) : null}
		</>
	);
}

export function ToolCardList({ parts }: { parts: ContentPart[] }) {
	const tools = parts.filter((part) => part.type === "tool");
	if (tools.length === 0) return null;

	return (
		<div className="mt-2 space-y-1.5">
			{tools.map((part, index) => (
				<ToolCard key={part.callID || `${part.tool || "tool"}-${index}`} part={part} />
			))}
		</div>
	);
}
