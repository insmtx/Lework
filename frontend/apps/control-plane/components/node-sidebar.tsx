"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Input } from "@lework/ui/components/ui/input";
import { cn } from "@lework/ui/lib/utils";
import { FolderGit2, Loader2, Plus, RefreshCw, Server, TerminalSquare } from "lucide-react";
import { useState } from "react";
import type { NodeView, Session } from "../lib/types";

interface NodeSidebarProps {
	nodes: NodeView[];
	selectedNodeID: string | null;
	onSelectNode: (nodeID: string) => void;
	sessions: Session[];
	selectedSessionID: string | null;
	onSelectSession: (sessionID: string) => void;
	onCreateSession: (directory: string) => Promise<void>;
	onRefresh: () => void;
	loadingSessions: boolean;
}

function shortTime(value?: string): string {
	if (!value) return "unknown";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return "unknown";
	return date.toLocaleString(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	});
}

function sessionTitle(session: Session): string {
	return session.title || session.id;
}

export function NodeSidebar({
	nodes,
	selectedNodeID,
	onSelectNode,
	sessions,
	selectedSessionID,
	onSelectSession,
	onCreateSession,
	onRefresh,
	loadingSessions,
}: NodeSidebarProps) {
	const [directory, setDirectory] = useState("");
	const [creating, setCreating] = useState(false);
	const [createError, setCreateError] = useState<string | null>(null);
	const selectedNode = nodes.find((node) => node.id === selectedNodeID);

	const submit = async (event: React.FormEvent) => {
		event.preventDefault();
		const value = directory.trim();
		if (!value || creating || !selectedNode?.connected) return;
		setCreating(true);
		setCreateError(null);
		try {
			await onCreateSession(value);
			setDirectory("");
		} catch (error) {
			setCreateError(error instanceof Error ? error.message : "Unable to create session");
		} finally {
			setCreating(false);
		}
	};

	return (
		<aside className="flex h-full w-full flex-col border-r bg-muted/25 md:w-80 md:min-w-80">
			<div className="flex items-center gap-3 border-b px-4 py-4">
				<div className="flex size-9 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
					<TerminalSquare className="size-5" />
				</div>
				<div className="min-w-0">
					<div className="text-sm font-semibold tracking-tight">Lework</div>
					<div className="text-xs text-muted-foreground">OpenCode control plane</div>
				</div>
			</div>

			<div className="border-b px-3 py-3">
				<div className="mb-2 flex items-center justify-between px-1">
					<span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
						Nodes
					</span>
					<span className="text-[11px] text-muted-foreground">{nodes.length}</span>
				</div>
				<div className="space-y-1">
					{nodes.length === 0 ? (
						<div className="rounded-lg border border-dashed p-3 text-xs text-muted-foreground">
							No node registered. Start <code className="text-[11px]">lework agent</code>.
						</div>
					) : (
						nodes.map((node) => (
							<button
								type="button"
								key={node.id}
								onClick={() => onSelectNode(node.id)}
								className={cn(
									"flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted",
									node.id === selectedNodeID && "bg-background shadow-sm ring-1 ring-border",
								)}
							>
								<span
									className={cn(
										"size-2 shrink-0 rounded-full",
										node.connected ? "bg-emerald-500" : "bg-muted-foreground/40",
									)}
								/>
								<span className="min-w-0 flex-1">
									<span className="block truncate text-sm font-medium">{node.name}</span>
									<span className="block truncate text-[11px] text-muted-foreground">
										{node.os}/{node.arch} · v{node.agent_version} ·{" "}
										{node.connected ? "online" : "offline"}
									</span>
									<span className="block text-[10px] text-muted-foreground/80">
										seen {shortTime(node.last_seen)}
									</span>
								</span>
								<Server className="size-4 shrink-0 text-muted-foreground/60" />
							</button>
						))
					)}
				</div>
			</div>

			<div className="flex min-h-0 flex-1 flex-col">
				<div className="flex items-center justify-between px-4 py-3">
					<span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
						Sessions
					</span>
					<Button
						variant="ghost"
						size="icon-xs"
						onClick={onRefresh}
						disabled={!selectedNode || loadingSessions}
						aria-label="Refresh sessions"
					>
						<RefreshCw className={cn("size-3.5", loadingSessions && "animate-spin")} />
					</Button>
				</div>

				<div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
					{!selectedNode ? (
						<p className="px-2 text-xs text-muted-foreground">Select a node to list sessions.</p>
					) : loadingSessions ? (
						<div className="flex items-center gap-2 px-2 py-3 text-xs text-muted-foreground">
							<Loader2 className="size-3.5 animate-spin" /> Loading sessions
						</div>
					) : sessions.length === 0 ? (
						<p className="px-2 text-xs text-muted-foreground">
							No sessions yet. Create one below with an absolute working directory.
						</p>
					) : (
						<div className="space-y-1">
							{sessions.map((session) => (
								<button
									type="button"
									key={session.id}
									onClick={() => onSelectSession(session.id)}
									className={cn(
										"flex w-full items-start gap-2 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted",
										session.id === selectedSessionID &&
											"bg-background shadow-sm ring-1 ring-border",
									)}
								>
									<FolderGit2 className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
									<span className="min-w-0 flex-1">
										<span className="block truncate text-sm font-medium">
											{sessionTitle(session)}
										</span>
										<span className="block truncate font-mono text-[10px] text-muted-foreground">
											{session.directory || "default directory"}
										</span>
										<span className="mt-0.5 block text-[10px] text-muted-foreground">
											{shortTime(session.updatedAt)}
										</span>
									</span>
								</button>
							))}
						</div>
					)}
				</div>

				<form onSubmit={submit} className="border-t p-3">
					<label
						className="mb-1.5 block text-[11px] font-medium text-muted-foreground"
						htmlFor="new-directory"
					>
						New session directory
					</label>
					<div className="flex gap-1.5">
						<Input
							id="new-directory"
							value={directory}
							onChange={(event) => setDirectory(event.target.value)}
							placeholder="/home/me/project"
							className="h-8 font-mono text-xs"
							disabled={!selectedNode?.connected || creating}
						/>
						<Button
							type="submit"
							size="icon"
							disabled={!selectedNode?.connected || creating || !directory.trim()}
							aria-label="Create session"
						>
							{creating ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<Plus className="size-4" />
							)}
						</Button>
					</div>
					{createError ? (
						<p className="mt-1.5 text-[11px] text-destructive">{createError}</p>
					) : null}
					{selectedNode && !selectedNode.connected ? (
						<p className="mt-1.5 text-[11px] text-muted-foreground">Node is offline.</p>
					) : null}
				</form>
			</div>
		</aside>
	);
}
