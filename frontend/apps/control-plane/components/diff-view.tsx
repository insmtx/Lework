"use client";

import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import { FileDiff, Loader2, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { sessionDiff } from "../lib/api";
import { type DiffRow, parsePatch } from "../lib/diff";
import type { FileDiff as FileDiffType } from "../lib/types";

interface DiffViewProps {
	nodeID: string;
	sessionID: string;
	directory: string;
}

function statusVariant(
	status: FileDiffType["status"],
): "default" | "secondary" | "destructive" | "outline" {
	if (status === "added") return "secondary";
	if (status === "deleted") return "destructive";
	return "outline";
}

function DiffRows({ rows }: { rows: DiffRow[] }) {
	return (
		<div className="min-w-[720px] font-mono text-[11px] leading-5">
			{rows.map((row, index) => {
				if (row.kind === "hunk") {
					return (
						<div key={index} className="border-y bg-muted/70 px-3 py-1 text-muted-foreground">
							{row.text}
						</div>
					);
				}
				const leftKind = row.kind === "add" ? "blank" : row.kind;
				const rightKind = row.kind === "delete" ? "blank" : row.kind;
				return (
					<div
						key={index}
						className="grid grid-cols-[3.25rem_minmax(0,1fr)_3.25rem_minmax(0,1fr)] border-b border-border/40"
					>
						<LineNo value={row.oldNo} className={leftKind === "delete" ? "bg-destructive/5" : ""} />
						<pre
							className={cn(
								"overflow-hidden px-2 py-0.5 whitespace-pre-wrap break-all",
								leftKind === "delete" && "bg-destructive/10 text-destructive",
								leftKind === "blank" && "bg-muted/20",
							)}
						>
							{leftKind === "blank" ? "" : row.text}
						</pre>
						<LineNo value={row.newNo} className={rightKind === "add" ? "bg-emerald-500/5" : ""} />
						<pre
							className={cn(
								"overflow-hidden px-2 py-0.5 whitespace-pre-wrap break-all",
								rightKind === "add" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
								rightKind === "blank" && "bg-muted/20",
							)}
						>
							{rightKind === "blank" ? "" : row.text}
						</pre>
					</div>
				);
			})}
		</div>
	);
}

function LineNo({ value, className }: { value: number | null; className?: string }) {
	return (
		<div className={cn("border-r bg-muted/30 px-1 text-right text-muted-foreground/70", className)}>
			{value ?? ""}
		</div>
	);
}

export function DiffView({ nodeID, sessionID, directory }: DiffViewProps) {
	const [diffs, setDiffs] = useState<FileDiffType[]>([]);
	const [selected, setSelected] = useState<string | null>(null);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const load = useCallback(async () => {
		setLoading(true);
		setError(null);
		try {
			const result = await sessionDiff(nodeID, sessionID, directory);
			setDiffs(result);
			setSelected((current) => {
				if (current && result.some((file) => file.file === current)) return current;
				return result[0]?.file ?? null;
			});
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "Unable to load diff");
		} finally {
			setLoading(false);
		}
	}, [nodeID, sessionID, directory]);

	useEffect(() => {
		void load();
	}, [load]);

	const current = useMemo(
		() => diffs.find((file) => file.file === selected) ?? null,
		[diffs, selected],
	);
	const rows = useMemo(() => (current ? parsePatch(current.patch) : []), [current]);

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div className="flex items-center gap-2 border-b px-4 py-2">
				<FileDiff className="size-4 text-muted-foreground" />
				<span className="text-xs font-medium">Working tree changes</span>
				<Badge variant="outline" className="ml-1">
					{diffs.length} files
				</Badge>
				<Button
					variant="ghost"
					size="icon-xs"
					className="ml-auto"
					onClick={() => void load()}
					disabled={loading}
					aria-label="Refresh diff"
				>
					<RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
				</Button>
			</div>

			{error ? (
				<div className="m-4 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
					{error}
				</div>
			) : loading && diffs.length === 0 ? (
				<div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
					<Loader2 className="size-4 animate-spin" /> Loading diff
				</div>
			) : diffs.length === 0 ? (
				<div className="m-auto max-w-sm p-6 text-center">
					<FileDiff className="mx-auto mb-3 size-8 text-muted-foreground/50" />
					<h3 className="text-sm font-medium">No changes in this turn</h3>
					<p className="mt-1 text-xs text-muted-foreground">
						OpenCode reports file changes per user turn. Ask it to edit a file and refresh.
					</p>
				</div>
			) : (
				<div className="flex min-h-0 flex-1">
					<div className="w-64 shrink-0 overflow-y-auto border-r p-2">
						{diffs.map((file) => (
							<button
								type="button"
								key={file.file}
								onClick={() => setSelected(file.file)}
								className={cn(
									"mb-0.5 flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-xs hover:bg-muted",
									file.file === selected && "bg-muted",
								)}
							>
								<span className="min-w-0 flex-1 truncate font-mono">{file.file}</span>
								<span className="shrink-0 text-[10px] text-emerald-600">+{file.additions}</span>
								<span className="shrink-0 text-[10px] text-destructive">-{file.deletions}</span>
							</button>
						))}
					</div>
					<div className="min-w-0 flex-1 overflow-auto">
						{current ? (
							<>
								<div className="flex items-center gap-2 border-b px-3 py-2">
									<code className="min-w-0 flex-1 truncate text-xs">{current.file}</code>
									<Badge variant={statusVariant(current.status)}>{current.status}</Badge>
								</div>
								<DiffRows rows={rows} />
							</>
						) : null}
					</div>
				</div>
			)}
		</div>
	);
}
