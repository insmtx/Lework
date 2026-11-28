"use client";

import { Button } from "@lework/ui/components/ui/button";
import { useIsMobile } from "@lework/ui/hooks/use-mobile";
import { cn } from "@lework/ui/lib/utils";
import {
	ChevronRight,
	FileCode2,
	FileText,
	Folder,
	Loader2,
	RefreshCw,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useControlApi } from "../context";
import type { FileInfo, FileReadResult } from "../types";
import { FilePreview } from "./file-preview";
import { cleanName, isSafeRelative, joinPath, parentPath } from "./file-paths";

interface FileBrowserProps {
	nodeID: string;
	directory: string;
}

export function FileBrowser({ nodeID, directory }: FileBrowserProps) {
	const isMobile = useIsMobile();
	const { client } = useControlApi();
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
			const result = await client.listFiles(nodeID, directory, path);
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
	}, [client, nodeID, directory, path]);

	useEffect(() => {
		setSelected(null);
		setFileError(null);
		void load();
	}, [load]);

	const openFile = async (entryPath: string) => {
		setFileLoading(true);
		setFileError(null);
		try {
			const result = await client.readFile(nodeID, directory, joinPath(path, entryPath));
			setSelected(result);
		} catch (caught) {
			setFileError(caught instanceof Error ? caught.message : "Unable to read file");
		} finally {
			setFileLoading(false);
		}
	};

	const segments = path.split("/").filter(Boolean);
	const crumbs = segments.map((segment, index) => ({
		segment,
		path: segments.slice(0, index + 1).join("/"),
	}));
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
							{crumbs.map((crumb) => (
								<span key={crumb.path} className="flex min-w-0 items-center">
									<ChevronRight className="size-3 shrink-0" />
									<button
										type="button"
										className="truncate hover:text-foreground"
										onClick={() => setPath(crumb.path)}
									>
										{crumb.segment}
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
						<FilePreview
							file={selected}
							onBack={isMobile ? () => setSelected(null) : undefined}
						/>
					)}
				</div>
			)}
		</div>
	);
}
