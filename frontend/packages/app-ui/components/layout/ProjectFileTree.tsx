"use client";

import { cn } from "@leros/ui/lib/utils";
import { ChevronRight, Download, Eye, Folder } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { renderHighlightedText } from "../common/searchText";
import { ProjectFileTypeIcon } from "./project-file-type-icon";
import {
	findProjectFileNode,
	getProjectFileFlatDisplayPathLabel,
	getProjectFileLocationLabel,
	getProjectFileSourceLabel,
	getProjectFileTypeLabel,
	getProjectFolderStats,
	PROJECT_FILE_TABLE_ACTIONS_CELL_CLASS,
	PROJECT_FILE_TABLE_GRID_CLASS,
	PROJECT_FILE_TABLE_LEADING_CELL_CLASS,
	PROJECT_FILE_TABLE_ROW_CLASS,
	type ProjectFileNode,
} from "./project-files";

type ProjectFileTreeProps = {
	nodes: ProjectFileNode[];
	variant?: "table" | "compact";
	layout?: "tree" | "flat";
	showFullPath?: boolean;
	searchKeyword?: string;
	fullTree?: ProjectFileNode[];
	projectId?: string;
	depth?: number;
	defaultExpanded?: boolean;
	onPreview?: (file: ProjectFileNode) => void;
	onDownload?: (file: ProjectFileNode) => void;
	formatBytes?: (size: number) => string;
	formatTime?: (timestamp: number) => string;
};

export function ProjectFileTree({
	nodes,
	variant = "table",
	layout = "tree",
	showFullPath = false,
	searchKeyword = "",
	fullTree,
	projectId,
	depth = 0,
	defaultExpanded = false,
	onPreview,
	onDownload,
	formatBytes = defaultFormatBytes,
	formatTime = defaultFormatTime,
}: ProjectFileTreeProps) {
	if (variant === "compact") {
		return (
			<div className="space-y-2">
				{nodes.map((node) => (
					<ProjectFileTreeCompactRow
						key={node.publicId || node.path}
						node={node}
						depth={depth}
						defaultExpanded={defaultExpanded}
						onPreview={onPreview}
						formatBytes={formatBytes}
						formatTime={formatTime}
					/>
				))}
			</div>
		);
	}

	return (
		<>
			{nodes.map((node) => (
				<ProjectFileTreeTableRow
					key={node.publicId || node.path}
					node={node}
					projectId={projectId}
					depth={layout === "flat" ? 0 : depth}
					layout={layout}
					showFullPath={showFullPath}
					searchKeyword={searchKeyword}
					fullTree={fullTree}
					defaultExpanded={defaultExpanded}
					onPreview={onPreview}
					onDownload={onDownload}
					formatBytes={formatBytes}
					formatTime={formatTime}
				/>
			))}
		</>
	);
}

function ProjectFileTreeTableRow({
	node,
	projectId,
	depth,
	layout,
	showFullPath,
	searchKeyword,
	fullTree,
	defaultExpanded,
	onPreview,
	onDownload,
	formatBytes,
	formatTime,
}: {
	node: ProjectFileNode;
	projectId?: string;
	depth: number;
	layout: "tree" | "flat";
	showFullPath: boolean;
	searchKeyword: string;
	fullTree?: ProjectFileNode[];
	defaultExpanded: boolean;
	onPreview?: (file: ProjectFileNode) => void;
	onDownload?: (file: ProjectFileNode) => void;
	formatBytes: (size: number) => string;
	formatTime: (timestamp: number) => string;
}) {
	const [expanded, setExpanded] = useState(defaultExpanded);
	useEffect(() => {
		if (defaultExpanded) {
			setExpanded(true);
		}
	}, [defaultExpanded, node.publicId]);

	const isDirectory = node.type === "directory";
	const sourceNode = fullTree ? findProjectFileNode(fullTree, node.publicId, node.path) : undefined;
	const treeChildren = sourceNode?.children ?? node.children;
	const canExpandFolder =
		isDirectory && treeChildren.length > 0 && (layout === "tree" || showFullPath);
	const expandChildrenAsTree = layout === "flat" && showFullPath;

	const treeIndent = layout === "flat" ? 0 : depth * 20;
	// 首级文件没有展开箭头，不再预留 24px 空槽，让文件名与表头「名称」左对齐。
	const isRootLevelFile = depth === 0 && !isDirectory;
	const folderStats = isDirectory && layout === "tree" ? getProjectFolderStats(node) : null;
	const displaySize = isDirectory ? (folderStats?.size ?? node.size ?? 0) : node.size;
	const displayCreatedAt = isDirectory
		? (folderStats?.createdAt ?? node.createdAt ?? 0)
		: node.createdAt;
	const pathLabel =
		layout === "flat"
			? showFullPath
				? getProjectFileFlatDisplayPathLabel(node)
				: getProjectFileLocationLabel(node)
			: "";

	return (
		<>
			<div className={cn(PROJECT_FILE_TABLE_GRID_CLASS, PROJECT_FILE_TABLE_ROW_CLASS)}>
				<div
					className={cn(
						"flex min-w-0 items-center gap-1 overflow-hidden",
						PROJECT_FILE_TABLE_LEADING_CELL_CLASS,
					)}
					style={treeIndent > 0 ? { paddingLeft: 20 + treeIndent } : undefined}
				>
					{canExpandFolder ? (
						<button
							type="button"
							onClick={() => setExpanded((value) => !value)}
							className="inline-flex size-6 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)]"
							aria-label={expanded ? "收起文件夹" : "展开文件夹"}
						>
							<ChevronRight
								className={cn("size-4 transition-transform", expanded && "rotate-90")}
							/>
						</button>
					) : isRootLevelFile ? null : (
						<span className="inline-block size-6 shrink-0" />
					)}
					{isDirectory ? (
						<div className="flex min-w-0 flex-1 items-center gap-2.5 px-2 py-1">
							<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								<Folder className="size-4" aria-hidden="true" />
							</div>
							<div className="min-w-0 flex-1">
								<p className="truncate text-[15px] font-semibold text-[var(--leros-text-strong)]">
									{node.name}
								</p>
								{pathLabel ? (
									<p className="truncate text-[12px] font-normal text-[var(--leros-text-muted)]">
										{renderSearchText(pathLabel, searchKeyword)}
									</p>
								) : null}
							</div>
						</div>
					) : (
						<button
							type="button"
							data-file-preview-trigger
							onClick={() => onPreview?.(node)}
							className={cn(
								"flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 rounded-lg py-1 text-left transition-colors hover:bg-[var(--leros-primary-softer)]/50",
								isRootLevelFile ? "pr-2" : "px-2",
							)}
							title="查看"
						>
							<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								<ProjectFileTypeIcon fileName={node.name} nodeType={node.nodeType} />
							</div>
							<div className="min-w-0 flex-1">
								<div className="flex min-w-0 items-center gap-1.5 text-[15px] font-semibold text-[var(--leros-text-strong)]">
									<span className="truncate">{node.name}</span>
									{node.versionNo > 0 ? (
										<span className="shrink-0 rounded bg-[var(--leros-primary-softer)] px-1.5 py-0.5 text-[10px] font-semibold leading-none text-[var(--leros-primary)]">
											V{node.versionNo}
										</span>
									) : null}
								</div>
								{pathLabel ? (
									<p className="truncate text-[12px] font-normal text-[var(--leros-text-muted)]">
										{renderSearchText(pathLabel, searchKeyword)}
									</p>
								) : null}
							</div>
						</button>
					)}
				</div>
				<div className="min-w-0 truncate text-[13px]">
					<span className="inline-block max-w-full truncate rounded-md bg-[var(--leros-surface-soft)] px-2.5 py-0.5 text-[11px] font-medium text-[var(--leros-text-muted)]">
						{getProjectFileSourceLabel(node)}
					</span>
				</div>
				<div className="min-w-0 truncate whitespace-nowrap text-[13px] text-[var(--leros-text-muted)]">
					{getProjectFileTypeLabel(node)}
				</div>
				<div className="min-w-0 truncate whitespace-nowrap text-[13px] text-[var(--leros-text-muted)]">
					{displaySize > 0 ? formatBytes(displaySize) : "-"}
				</div>
				<div className="min-w-0 truncate whitespace-nowrap text-[13px] text-[var(--leros-text-muted)]">
					{displayCreatedAt ? formatTime(displayCreatedAt) : "-"}
				</div>
				<div className={PROJECT_FILE_TABLE_ACTIONS_CELL_CLASS}>
					{isDirectory ? (
						<button
							type="button"
							onClick={() => onDownload?.(node)}
							className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[13px] text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
							title="下载"
						>
							<Download className="size-4" />
							下载
						</button>
					) : (
						<>
							<button
								type="button"
								data-file-preview-trigger
								onClick={() => onPreview?.(node)}
								className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[13px] text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
								title="查看"
							>
								<Eye className="size-4" />
								查看
							</button>
							<button
								type="button"
								onClick={() => onDownload?.(node)}
								className="inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[13px] text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
								title="下载"
							>
								<Download className="size-4" />
								下载
							</button>
						</>
					)}
				</div>
			</div>
			{canExpandFolder && expanded ? (
				<ProjectFileTree
					nodes={treeChildren}
					variant="table"
					layout={expandChildrenAsTree ? "tree" : layout}
					showFullPath={false}
					searchKeyword={searchKeyword}
					fullTree={fullTree}
					projectId={projectId}
					depth={depth + 1}
					defaultExpanded={defaultExpanded}
					onPreview={onPreview}
					onDownload={onDownload}
					formatBytes={formatBytes}
					formatTime={formatTime}
				/>
			) : null}
		</>
	);
}

function ProjectFileTreeCompactRow({
	node,
	depth,
	defaultExpanded,
	onPreview,
	formatBytes,
	formatTime,
}: {
	node: ProjectFileNode;
	depth: number;
	defaultExpanded: boolean;
	onPreview?: (file: ProjectFileNode) => void;
	formatBytes: (size: number) => string;
	formatTime: (timestamp: number) => string;
}) {
	const [expanded, setExpanded] = useState(defaultExpanded);
	useEffect(() => {
		if (defaultExpanded) {
			setExpanded(true);
		}
	}, [defaultExpanded, node.publicId]);
	const isDirectory = node.type === "directory";

	if (isDirectory) {
		return (
			<div>
				<button
					type="button"
					onClick={() => setExpanded((value) => !value)}
					className="flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm font-medium text-[var(--leros-text-strong)] transition-colors hover:bg-[var(--leros-primary-softer)]/35"
					style={{ paddingLeft: 8 + depth * 14 }}
				>
					<ChevronRight
						className={cn(
							"size-3.5 shrink-0 text-[var(--leros-text-muted)] transition-transform",
							expanded && "rotate-90",
						)}
					/>
					<Folder className="size-4 shrink-0 text-[var(--leros-primary)]" />
					<span className="truncate">{node.name}</span>
				</button>
				{expanded && node.children.length > 0 ? (
					<ProjectFileTree
						nodes={node.children}
						variant="compact"
						depth={depth + 1}
						defaultExpanded={defaultExpanded}
						onPreview={onPreview}
						formatBytes={formatBytes}
						formatTime={formatTime}
					/>
				) : null}
			</div>
		);
	}

	return (
		<button
			type="button"
			data-file-preview-trigger
			onClick={() => onPreview?.(node)}
			className="group relative flex w-full cursor-pointer items-center gap-3 overflow-hidden rounded-lg border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3.5 py-3 text-left shadow-sm transition-colors hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/35"
			style={{ marginLeft: depth * 14 }}
			title="预览文件"
		>
			<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)]">
				<ProjectFileTypeIcon
					fileName={node.name}
					nodeType={node.nodeType}
					className="size-5 object-contain"
				/>
			</div>
			<div className="min-w-0">
				<div className="flex min-w-0 items-center gap-1.5 text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
					<span className="truncate">{node.name}</span>
					{node.versionNo > 0 ? (
						<span className="shrink-0 rounded bg-[var(--leros-primary-softer)] px-1.5 py-0.5 text-[10px] font-semibold leading-none text-[var(--leros-primary)]">
							V{node.versionNo}
						</span>
					) : null}
				</div>
				<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
					{[
						node.size > 0 ? formatBytes(node.size) : "",
						node.createdAt ? formatTime(node.createdAt) : "",
					]
						.filter(Boolean)
						.join(" · ")}
				</div>
			</div>
		</button>
	);
}

function renderSearchText(text: string, keyword: string): ReactNode {
	if (!keyword.trim()) {
		return text;
	}
	return renderHighlightedText(text, keyword);
}

function defaultFormatBytes(size: number): string {
	if (!size) return "";
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function defaultFormatTime(timestamp: number): string {
	if (!timestamp) return "";
	return new Date(timestamp).toLocaleString("zh-CN");
}
