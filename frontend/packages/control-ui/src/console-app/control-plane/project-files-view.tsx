"use client";

import { Button } from "@lework/ui/components/ui/button";
import { useIsMobile } from "@lework/ui/hooks/use-mobile";
import { cn } from "@lework/ui/lib/utils";
import { ChevronLeft, ChevronRight, FileText, Folder, Loader2, RefreshCw } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { cleanName, isSafeRelative, joinPath } from "../../components/file-paths";
import { FilePreview } from "../../components/file-preview";
import { useControlApi } from "../../context";
import type { FileInfo, FileReadResult, NodeView, ProjectSummary } from "../../types";
import { projectNodeCanonical, resolveProjectNode } from "./project-node";

/** baseName 取路径最后一段，兼容 Windows 分隔符。 */
function baseName(path: string): string {
	const parts = path.split(/[\\/]/).filter(Boolean);
	return parts[parts.length - 1] ?? path;
}

/** sortEntries 保证目录在前，同级按路径稳定排序。 */
function sortEntries(entries: FileInfo[]): FileInfo[] {
	return [...entries].sort((a, b) => {
		if (a.type !== b.type) return a.type === "directory" ? -1 : 1;
		return a.path.localeCompare(b.path);
	});
}

/**
 * ProjectFilesView 是工作区级别的只读文件树。
 *
 * 目录按需展开，每个层级只请求一次；文件内容与下载仍由节点 agent 提供，
 * 页面仅缓存本次浏览的结果，不复制任何文件状态。
 */
export function ProjectFilesView({
	projectKey,
	onBack,
}: {
	projectKey: string;
	onBack: () => void;
}) {
	const { client } = useControlApi();
	const isMobile = useIsMobile();
	const [project, setProject] = useState<ProjectSummary | null>(null);
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [dirError, setDirError] = useState<string | null>(null);
	const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null);
	const [cache, setCache] = useState<Record<string, FileInfo[]>>({});
	const [expanded, setExpanded] = useState<Record<string, true>>({});
	const [pendingDirs, setPendingDirs] = useState<Record<string, true>>({});
	const [selected, setSelected] = useState<FileReadResult | null>(null);
	const [fileLoading, setFileLoading] = useState(false);
	const [fileError, setFileError] = useState<string | null>(null);

	useEffect(() => {
		let cancelled = false;
		setLoading(true);
		Promise.all([client.listProjects(), client.listNodes()])
			.then(([projects, nodeList]) => {
				if (cancelled) return;
				setNodes(nodeList);
				const found = projects.find((item) => item.key === projectKey) ?? null;
				setProject(found);
				if (!found) {
					setError("没有找到这个工作区，它可能已从节点上移除。");
					return;
				}
				setSelectedNodeID(resolveProjectNode(found)?.node.node_id ?? null);
			})
			.catch((caught) => {
				if (cancelled) return;
				setError(caught instanceof Error ? caught.message : "无法加载工作区");
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [client, projectKey]);

	const canonical =
		project && selectedNodeID ? projectNodeCanonical(project, selectedNodeID) : null;
	const selectedProjectNode = useMemo(
		() => project?.nodes.find((node) => node.node_id === selectedNodeID) ?? null,
		[project, selectedNodeID],
	);
	const canBrowse = Boolean(selectedProjectNode?.online && canonical);

	const loadDir = useCallback(
		async (dir: string) => {
			if (!selectedNodeID || !canonical) return;
			setPendingDirs((prev) => ({ ...prev, [dir]: true }));
			try {
				const entries = await client.listFiles(selectedNodeID, canonical, dir);
				setDirError(null);
				setCache((prev) => ({ ...prev, [dir]: entries }));
			} catch (caught) {
				setDirError(caught instanceof Error ? caught.message : "无法读取目录");
			} finally {
				setPendingDirs((prev) => {
					const next = { ...prev };
					delete next[dir];
					return next;
				});
			}
		},
		[canonical, client, selectedNodeID],
	);

	useEffect(() => {
		if (!canBrowse) return;
		setCache({});
		setExpanded({});
		setSelected(null);
		setFileError(null);
		setDirError(null);
		void loadDir("");
	}, [canBrowse, loadDir]);

	const toggleDir = (dir: string) => {
		if (expanded[dir]) {
			setExpanded((prev) => {
				const next = { ...prev };
				delete next[dir];
				return next;
			});
			return;
		}
		setExpanded((prev) => ({ ...prev, [dir]: true }));
		if (cache[dir] === undefined && !pendingDirs[dir]) void loadDir(dir);
	};

	const openFile = async (path: string) => {
		if (!selectedNodeID || !canonical || !isSafeRelative(path)) return;
		setFileLoading(true);
		setFileError(null);
		try {
			setSelected(await client.readFile(selectedNodeID, canonical, path));
		} catch (caught) {
			setFileError(caught instanceof Error ? caught.message : "无法读取文件");
		} finally {
			setFileLoading(false);
		}
	};

	const reload = () => {
		setCache({});
		setExpanded({});
		setSelected(null);
		setFileError(null);
		setDirError(null);
		void loadDir("");
	};

	const nodeName = (nodeID: string) =>
		nodes.find((node) => node.id === nodeID)?.name ?? nodeID.slice(0, 8);

	const renderLevel = (dir: string, depth: number): ReactNode => {
		const entries = cache[dir];
		if (entries === undefined) {
			return pendingDirs[dir] ? (
				<div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
					<Loader2 className="size-3.5 animate-spin" /> 正在加载…
				</div>
			) : null;
		}
		if (entries.length === 0) {
			return (
				<p
					className="px-3 py-2 text-xs text-muted-foreground"
					style={{ paddingLeft: `${12 + depth * 14}px` }}
				>
					此目录为空
				</p>
			);
		}
		return sortEntries(entries).map((entry) => {
			const name = cleanName(entry.path);
			const full = joinPath(dir, entry.path);
			const isDir = entry.type === "directory";
			return (
				<div key={full}>
					<button
						type="button"
						onClick={() => (isDir ? toggleDir(full) : void openFile(full))}
						className={cn(
							"flex w-full items-center gap-1.5 rounded-md py-1.5 pr-2 text-left text-xs hover:bg-muted",
							isDir && "font-medium",
						)}
						style={{ paddingLeft: `${8 + depth * 14}px` }}
					>
						{isDir ? (
							<ChevronRight
								className={cn(
									"size-3.5 shrink-0 transition-transform",
									expanded[full] && "rotate-90",
								)}
							/>
						) : (
							<span className="size-3.5 shrink-0" />
						)}
						{isDir ? (
							<Folder className="size-3.5 shrink-0 text-amber-600" />
						) : (
							<FileText className="size-3.5 shrink-0 text-muted-foreground" />
						)}
						<span className="truncate font-mono">{name}</span>
					</button>
					{isDir && expanded[full] ? renderLevel(full, depth + 1) : null}
				</div>
			);
		});
	};

	if (error) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3 bg-[var(--lework-app-bg)] p-6 text-center">
				<p className="text-sm text-[var(--lework-text-muted)]">{error}</p>
				<Button variant="outline" onClick={onBack}>
					返回工作区
				</Button>
			</div>
		);
	}

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center gap-2 bg-[var(--lework-app-bg)] text-sm text-[var(--lework-text-muted)]">
				<Loader2 className="size-4 animate-spin" /> 正在载入文件…
			</div>
		);
	}

	if (project && !canBrowse) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3 bg-[var(--lework-app-bg)] p-6 text-center">
				<p className="text-sm text-[var(--lework-text-muted)]">没有在线节点，无法浏览文件。</p>
				<Button variant="outline" onClick={onBack}>
					返回工作区
				</Button>
			</div>
		);
	}

	const displayName = project
		? project.alias?.trim() || baseName(project.canonical) || project.id
		: "";
	const showPreviewOnly = isMobile && selected !== null;
	const showListOnly = isMobile && selected === null;

	return (
		<div className="flex h-full min-h-0 flex-col bg-[var(--lework-app-bg)]">
			<header className="flex shrink-0 items-center gap-3 border-b border-[var(--lework-control-border)] bg-white px-4 py-2.5">
				<Button variant="ghost" size="sm" className="gap-1.5" onClick={onBack}>
					<ChevronLeft className="size-4" /> 返回工作区
				</Button>
				<div className="min-w-0 flex-1">
					<p className="truncate text-sm font-semibold text-[var(--lework-text-strong)]">
						{displayName}
					</p>
					<code className="block truncate text-[11px] text-[var(--lework-text-muted)]">
						{canonical}
					</code>
				</div>
				{project && project.nodes.length > 1 ? (
					<select
						aria-label="选择节点"
						className="h-8 max-w-40 rounded-md border border-[var(--lework-control-border)] bg-white px-2 text-xs"
						value={selectedNodeID ?? ""}
						onChange={(event) => setSelectedNodeID(event.target.value)}
					>
						{project.nodes.map((node) => (
							<option key={node.node_id} value={node.node_id} disabled={!node.online}>
								{nodeName(node.node_id)}
								{node.online ? "" : "（离线）"}
							</option>
						))}
					</select>
				) : null}
				<Button variant="ghost" size="icon-sm" onClick={reload} aria-label="刷新文件">
					<RefreshCw className="size-3.5" />
				</Button>
			</header>

			<div className="flex min-h-0 flex-1">
				{showPreviewOnly ? null : (
					<div
						className={cn(
							"flex flex-col border-r border-[var(--lework-control-border)]",
							isMobile ? "min-h-0 flex-1 border-r-0" : "w-72 shrink-0",
						)}
					>
						{dirError ? (
							<p className="border-b bg-destructive/5 px-3 py-1.5 text-[11px] text-destructive">
								{dirError}
							</p>
						) : null}
						<div className="min-h-0 flex-1 overflow-y-auto p-1.5">{renderLevel("", 0)}</div>
					</div>
				)}

				{showListOnly ? null : (
					<div className="min-w-0 flex-1 overflow-auto">
						{fileLoading ? (
							<div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
								<Loader2 className="size-4 animate-spin" /> 正在读取文件…
							</div>
						) : fileError ? (
							<div className="m-4 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
								{fileError}
							</div>
						) : !selected ? (
							<div className="m-auto max-w-sm p-6 text-center text-sm text-muted-foreground">
								选择一个文件查看内容
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
		</div>
	);
}
