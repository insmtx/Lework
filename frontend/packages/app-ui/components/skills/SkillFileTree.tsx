"use client";

import type { PluginRevisionFile } from "@leros/store";
import { ChevronRight, FileText, Folder } from "lucide-react";
import { useMemo, useState } from "react";
import { buildSkillFileTree, type SkillFileTreeNode } from "./skillFileTreeModel";

export function SkillFileTree({ files }: { files: PluginRevisionFile[] }) {
	const tree = useMemo(() => buildSkillFileTree(files), [files]);
	const [expanded, setExpanded] = useState<Set<string>>(() => new Set());

	const toggleDirectory = (path: string) => {
		setExpanded((current) => {
			const next = new Set(current);
			if (next.has(path)) next.delete(path);
			else next.add(path);
			return next;
		});
	};

	return (
		<div className="space-y-0.5">
			{tree.map((node) => (
				<SkillFileTreeRow
					key={`${node.type}:${node.path}`}
					node={node}
					depth={0}
					expanded={expanded}
					onToggle={toggleDirectory}
				/>
			))}
		</div>
	);
}

function SkillFileTreeRow({
	node,
	depth,
	expanded,
	onToggle,
}: {
	node: SkillFileTreeNode;
	depth: number;
	expanded: Set<string>;
	onToggle: (path: string) => void;
}) {
	const isExpanded = expanded.has(node.path);
	if (node.type === "directory") {
		return (
			<div>
				<button
					type="button"
					onClick={() => onToggle(node.path)}
					className="flex w-full min-w-0 items-center gap-1.5 rounded-md py-2 pr-3 text-left text-xs text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-surface-soft)]"
					style={{ paddingLeft: `${12 + depth * 18}px` }}
				>
					<ChevronRight
						className={`size-3.5 shrink-0 text-[var(--leros-text-subtle)] transition-transform ${isExpanded ? "rotate-90" : ""}`}
					/>
					<Folder className="size-3.5 shrink-0 text-[var(--leros-primary)]" />
					<span className="min-w-0 truncate font-mono">{node.name}</span>
				</button>
				{isExpanded &&
					node.children.map((child) => (
						<SkillFileTreeRow
							key={`${child.type}:${child.path}`}
							node={child}
							depth={depth + 1}
							expanded={expanded}
							onToggle={onToggle}
						/>
					))}
			</div>
		);
	}

	return (
		<div
			className="flex min-w-0 items-center gap-2 rounded-md py-2 pr-3 text-xs text-[var(--leros-text-muted)]"
			style={{ paddingLeft: `${30 + depth * 18}px` }}
			title={node.path}
		>
			<FileText className="size-3.5 shrink-0 text-[var(--leros-text-subtle)]" />
			<span className="min-w-0 truncate font-mono">{node.name}</span>
			{node.file && (
				<span className="ml-auto shrink-0 text-[10px] text-[var(--leros-text-subtle)]">
					{formatFileSize(node.file.size_bytes)}
				</span>
			)}
		</div>
	);
}

function formatFileSize(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
	return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
