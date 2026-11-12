"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Input } from "@lework/ui/components/ui/input";
import { cn } from "@lework/ui/lib/utils";
import {
	Check,
	FolderGit2,
	Loader2,
	Pencil,
	Play,
	Plus,
	RefreshCw,
	RotateCcw,
	Search,
	Server,
	Square,
	TerminalSquare,
	Trash2,
	X,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { NodeHealth, NodeView, Session } from "../lib/types";

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
	sessionDirectory: string;
	onFilterSessions: (directory: string) => void;
	onRenameSession: (sessionID: string, title: string) => Promise<void>;
	onDeleteSession: (sessionID: string) => Promise<void>;
	nodeHealth: NodeHealth | null;
	nodeHealthError: string | null;
	lifecycleBusy: "start" | "restart" | "stop" | null;
	onStartOpenCode: () => void | Promise<void>;
	onRestartOpenCode: () => void | Promise<void>;
	onStopOpenCode: () => void | Promise<void>;
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
	sessionDirectory,
	onFilterSessions,
	onRenameSession,
	onDeleteSession,
	nodeHealth,
	nodeHealthError,
	lifecycleBusy,
	onStartOpenCode,
	onRestartOpenCode,
	onStopOpenCode,
}: NodeSidebarProps) {
	const [directory, setDirectory] = useState("");
	const [creating, setCreating] = useState(false);
	const [createError, setCreateError] = useState<string | null>(null);
	const [filterDraft, setFilterDraft] = useState(sessionDirectory);
	const [renamingID, setRenamingID] = useState<string | null>(null);
	const [renameDraft, setRenameDraft] = useState("");
	const [renaming, setRenaming] = useState(false);
	const [confirmDeleteID, setConfirmDeleteID] = useState<string | null>(null);
	const [deletingID, setDeletingID] = useState<string | null>(null);
	const [actionError, setActionError] = useState<string | null>(null);
	const selectedNode = nodes.find((node) => node.id === selectedNodeID);

	useEffect(() => {
		setFilterDraft(sessionDirectory);
	}, [sessionDirectory]);

	const submitFilter = (event: React.FormEvent) => {
		event.preventDefault();
		onFilterSessions(filterDraft.trim());
	};

	const startRename = (session: Session) => {
		setRenamingID(session.id);
		setRenameDraft(sessionTitle(session));
		setConfirmDeleteID(null);
		setActionError(null);
	};

	const submitRename = async (event: React.FormEvent) => {
		event.preventDefault();
		const value = renameDraft.trim();
		if (!renamingID || !value || renaming) return;
		setRenaming(true);
		setActionError(null);
		try {
			await onRenameSession(renamingID, value);
			setRenamingID(null);
		} catch (error) {
			setActionError(error instanceof Error ? error.message : "Unable to rename session");
		} finally {
			setRenaming(false);
		}
	};

	const removeSession = async (sessionID: string) => {
		if (deletingID !== null) return;
		setDeletingID(sessionID);
		setActionError(null);
		try {
			await onDeleteSession(sessionID);
			setConfirmDeleteID(null);
		} catch (error) {
			setActionError(error instanceof Error ? error.message : "Unable to delete session");
		} finally {
			setDeletingID(null);
		}
	};

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

			{selectedNode ? (
				<div className="border-b px-3 py-3">
					<div className="mb-2 flex items-center justify-between px-1">
						<span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
							OpenCode
						</span>
						<span
							className={cn(
								"rounded-full px-1.5 py-0.5 text-[10px] font-medium",
								nodeHealth?.running
									? "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
									: nodeHealth
										? "bg-muted text-muted-foreground"
										: "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200",
							)}
						>
							{nodeHealth ? (nodeHealth.running ? "running" : "stopped") : "unknown"}
						</span>
					</div>
					<div className="flex gap-1.5">
						<Button
							size="sm"
							variant="outline"
							className="h-7 flex-1 text-xs"
							onClick={() => void onStartOpenCode()}
							disabled={
								!selectedNode.connected || lifecycleBusy !== null || nodeHealth?.running === true
							}
						>
							{lifecycleBusy === "start" ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<Play className="size-3.5" />
							)}
							Start
						</Button>
						<Button
							size="sm"
							variant="outline"
							className="h-7 flex-1 text-xs"
							onClick={() => void onRestartOpenCode()}
							disabled={!selectedNode.connected || lifecycleBusy !== null}
						>
							{lifecycleBusy === "restart" ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<RotateCcw className="size-3.5" />
							)}
							Restart
						</Button>
						<Button
							size="sm"
							variant="outline"
							className="h-7 flex-1 text-xs"
							onClick={() => void onStopOpenCode()}
							disabled={
								!selectedNode.connected || lifecycleBusy !== null || nodeHealth?.running === false
							}
						>
							{lifecycleBusy === "stop" ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<Square className="size-3.5" />
							)}
							Stop
						</Button>
					</div>
					{nodeHealth?.base_url ? (
						<p className="mt-1.5 truncate px-1 font-mono text-[10px] text-muted-foreground">
							{nodeHealth.base_url}
						</p>
					) : null}
					{nodeHealthError ? (
						<p className="mt-1.5 px-1 text-[11px] text-destructive">{nodeHealthError}</p>
					) : null}
				</div>
			) : null}

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

				{selectedNode ? (
					<form onSubmit={submitFilter} className="flex gap-1.5 px-3 pb-2">
						<Input
							value={filterDraft}
							onChange={(event) => setFilterDraft(event.target.value)}
							placeholder="Filter by directory"
							className="h-7 font-mono text-[11px]"
							aria-label="Filter sessions by directory"
						/>
						{sessionDirectory ? (
							<Button
								type="button"
								size="icon-xs"
								variant="ghost"
								aria-label="Clear session filter"
								onClick={() => {
									setFilterDraft("");
									onFilterSessions("");
								}}
							>
								<X className="size-3.5" />
							</Button>
						) : null}
						<Button
							type="submit"
							size="icon-xs"
							variant="ghost"
							aria-label="Apply session filter"
							disabled={filterDraft.trim() === sessionDirectory}
						>
							<Search className="size-3.5" />
						</Button>
					</form>
				) : null}

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
								<div
									key={session.id}
									className={cn(
										"rounded-lg transition-colors",
										session.id === selectedSessionID &&
											"bg-background shadow-sm ring-1 ring-border",
									)}
								>
									{renamingID === session.id ? (
										<form onSubmit={submitRename} className="flex items-center gap-1 p-1.5">
											<Input
												autoFocus
												value={renameDraft}
												onChange={(event) => setRenameDraft(event.target.value)}
												onKeyDown={(event) => {
													if (event.key === "Escape") setRenamingID(null);
												}}
												className="h-7 font-mono text-xs"
												aria-label="Session title"
											/>
											<Button
												type="submit"
												size="icon-xs"
												disabled={renaming || !renameDraft.trim()}
												aria-label="Save session title"
											>
												{renaming ? (
													<Loader2 className="size-3.5 animate-spin" />
												) : (
													<Check className="size-3.5" />
												)}
											</Button>
											<Button
												type="button"
												size="icon-xs"
												variant="ghost"
												onClick={() => setRenamingID(null)}
												aria-label="Cancel rename"
											>
												<X className="size-3.5" />
											</Button>
										</form>
									) : (
										<div className="flex items-start gap-1 px-1 py-1">
											<button
												type="button"
												onClick={() => onSelectSession(session.id)}
												className="flex min-w-0 flex-1 items-start gap-2 rounded-md px-1 py-1 text-left transition-colors hover:bg-muted"
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
											<div className="flex shrink-0 items-center gap-0.5 pt-0.5">
												<Button
													size="icon-xs"
													variant="ghost"
													onClick={() => startRename(session)}
													aria-label="Rename session"
												>
													<Pencil className="size-3.5" />
												</Button>
												{confirmDeleteID === session.id ? (
													<>
														<Button
															size="icon-xs"
															variant="destructive"
															onClick={() => void removeSession(session.id)}
															disabled={deletingID !== null}
															aria-label="Confirm delete session"
														>
															{deletingID === session.id ? (
																<Loader2 className="size-3.5 animate-spin" />
															) : (
																<Check className="size-3.5" />
															)}
														</Button>
														<Button
															size="icon-xs"
															variant="ghost"
															onClick={() => setConfirmDeleteID(null)}
															aria-label="Cancel delete"
														>
															<X className="size-3.5" />
														</Button>
													</>
												) : (
													<Button
														size="icon-xs"
														variant="ghost"
														onClick={() => {
															setConfirmDeleteID(session.id);
															setRenamingID(null);
														}}
														aria-label="Delete session"
													>
														<Trash2 className="size-3.5" />
													</Button>
												)}
											</div>
										</div>
									)}
								</div>
							))}
						</div>
					)}
				</div>

				{actionError ? (
					<p className="border-t px-3 py-1.5 text-[11px] text-destructive">{actionError}</p>
				) : null}

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
