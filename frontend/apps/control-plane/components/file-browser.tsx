"use client";

import { Button } from "@lework/ui/components/ui/button";
import { useIsMobile } from "@lework/ui/hooks/use-mobile";
import { cn } from "@lework/ui/lib/utils";
import {
	ChevronLeft,
	ChevronRight,
	Download,
	FileCode2,
	FileText,
	Folder,
	Loader2,
	RefreshCw,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { listFiles, readFile } from "../lib/api";
import type { FileInfo, FileReadResult } from "../lib/types";

interface FileBrowserProps {
	nodeID: string;
	directory: string;
}

function cleanName(path: string): string {
	return path.endsWith("/") ? path.slice(0, -1) : path;
}

function isSafeRelative(path: string): boolean {
	return path !== "" && !path.startsWith("/") && !path.split("/").includes("..");
}

function joinPath(base: string, name: string): string {
	const clean = cleanName(name);
	return base ? `${base}/${clean}` : clean;
}

function parentPath(path: string): string {
	const parts = path.split("/").filter(Boolean);
	parts.pop();
	return parts.join("/");
}

function formatBytes(value: number): string {
	if (value < 1024) return `${value} B`;
	if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
	return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

export function FileBrowser({ nodeID, directory }: FileBrowserProps) {
	const isMobile = useIsMobile();
	const [path, setPath] = useState("");
	const [entries, setEntries] = useState<FileInfo[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [selected, setSelected] = useState<FileReadResult | null>(null);
	const [fileLoading, setFileLoading] = useState(false);
	const [fileError, setFileError] = useState<string | null>(null);

	const load = useCallback(async () => {
		setLoading(true);
		setError(null);
		try {
			const result = await listFiles(nodeID, directory, path);
			setEntries(
				[...result].sort((a, b) => {
					if (a.type !== b.type) return a.type === "directory" ? -1 : 1;
					return a.path.localeCompare(b.path);
				}),
			);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "Unable to list files");
		} finally {
			setLoading(false);
		}
	}, [nodeID, directory, path]);

	useEffect(() => {
		setSelected(null);
		setFileError(null);
		void load();
	}, [load]);

	const openFile = async (entryPath: string) => {
		setFileLoading(true);
		setFileError(null);
		try {
			const result = await readFile(nodeID, directory, joinPath(path, entryPath));
			setSelected(result);
		} catch (caught) {
			setFileError(caught instanceof Error ? caught.message : "Unable to read file");
		} finally {
			setFileLoading(false);
		}
	};

	const segments = path.split("/").filter(Boolean);
	// 手机屏幕只有单栏：有选中文件时让预览占满，返回按钮回到列表。
	const showPreviewOnly = isMobile && selected !== null;
	const showListOnly = isMobile && selected === null;

	return (
		<div className="flex min-h-0 flex-1">
			{showPreviewOnly ? null : (
				<div
					className={cn(
						"flex flex-col border-r",
						isMobile ? "min-h-0 flex-1 border-r-0" : "w-72 shrink-0",
					)}
				>
					<div className="flex items-center gap-1 border-b px-2 py-2">
						<Button variant="ghost" size="icon-xs" onClick={() => setPath("")} title="Session root">
							<Folder className="size-3.5" />
						</Button>
						<div className="flex min-w-0 flex-1 items-center overflow-hidden text-[11px] text-muted-foreground">
							<button
								type="button"
								className="shrink-0 hover:text-foreground"
								onClick={() => setPath("")}
							>
								root
							</button>
							{segments.map((segment, index) => (
								<span key={`${segment}-${index}`} className="flex min-w-0 items-center">
									<ChevronRight className="size-3 shrink-0" />
									<button
										type="button"
										className="truncate hover:text-foreground"
										onClick={() => setPath(segments.slice(0, index + 1).join("/"))}
									>
										{segment}
									</button>
								</span>
							))}
						</div>
						<Button
							variant="ghost"
							size="icon-xs"
							onClick={() => void load()}
							disabled={loading}
							aria-label="Refresh files"
						>
							<RefreshCw className={cn("size-3.5", loading && "animate-spin")} />
						</Button>
					</div>
					<div className="min-h-0 flex-1 overflow-y-auto p-1.5">
						{path ? (
							<button
								type="button"
								className="mb-0.5 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-muted-foreground hover:bg-muted"
								onClick={() => setPath(parentPath(path))}
							>
								<ChevronRight className="size-3 rotate-180" /> ..
							</button>
						) : null}
						{error ? (
							<p className="p-2 text-xs text-destructive">{error}</p>
						) : loading && entries.length === 0 ? (
							<div className="flex items-center gap-2 p-2 text-xs text-muted-foreground">
								<Loader2 className="size-3.5 animate-spin" /> Loading
							</div>
						) : entries.length === 0 ? (
							<p className="p-2 text-xs text-muted-foreground">Empty directory</p>
						) : (
							entries.map((entry) => (
								<button
									type="button"
									key={entry.path}
									onClick={() => {
										if (!isSafeRelative(entry.path)) return;
										if (entry.type === "directory") {
											setPath(joinPath(path, entry.path));
										} else {
											void openFile(entry.path);
										}
									}}
									className={cn(
										"mb-0.5 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted",
										entry.type === "directory" && "font-medium",
									)}
								>
									{entry.type === "directory" ? (
										<Folder className="size-3.5 shrink-0 text-amber-600" />
									) : (
										<FileText className="size-3.5 shrink-0 text-muted-foreground" />
									)}
									<span className="truncate font-mono">{cleanName(entry.path)}</span>
								</button>
							))
						)}
					</div>
				</div>
			)}

			{showListOnly ? null : (
				<div className="min-w-0 flex-1 overflow-auto">
					{fileLoading ? (
						<div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
							<Loader2 className="size-4 animate-spin" /> Reading file
						</div>
					) : fileError ? (
						<div className="m-4 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
							{fileError}
						</div>
					) : !selected ? (
						<div className="m-auto max-w-sm p-6 text-center">
							<FileCode2 className="mx-auto mb-3 size-8 text-muted-foreground/50" />
							<h3 className="text-sm font-medium">Browse the session directory</h3>
							<p className="mt-1 text-xs text-muted-foreground">
								Files are read through the node agent, limited to 512 KiB per request.
							</p>
						</div>
					) : (
						<div className="flex h-full flex-col">
							<div className="flex items-center gap-2 border-b px-3 py-2">
								{isMobile ? (
									<Button
										variant="ghost"
										size="icon-xs"
										onClick={() => setSelected(null)}
										aria-label="Back to files"
									>
										<ChevronLeft className="size-4" />
									</Button>
								) : null}
								<code className="min-w-0 flex-1 truncate text-xs">{selected.path}</code>
								<span className="text-[10px] text-muted-foreground">
									{formatBytes(selected.size)}
								</span>
								{selected.encoding === "base64" ? (
									<a
										className="inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs hover:bg-muted"
										href={`data:application/octet-stream;base64,${selected.content}`}
										download={selected.path.split("/").pop() || "download"}
									>
										<Download className="size-3" /> Download
									</a>
								) : null}
							</div>
							{selected.truncated ? (
								<div className="border-b bg-amber-50 px-3 py-1.5 text-[11px] text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
									Showing the first 512 KiB of this file.
								</div>
							) : null}
							{selected.encoding === "utf-8" ? (
								<pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words bg-background p-4 font-mono text-xs leading-relaxed">
									{selected.content}
								</pre>
							) : (
								<div className="m-auto p-6 text-center text-sm text-muted-foreground">
									<FileText className="mx-auto mb-2 size-8 opacity-50" />
									Binary file ({formatBytes(selected.size)})
								</div>
							)}
						</div>
					)}
				</div>
			)}
		</div>
	);
}
