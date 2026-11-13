"use client";

import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import {
	ChevronLeft,
	FileDiff,
	FolderTree,
	Loader2,
	MessageSquare,
	RefreshCw,
	Square,
	SquareTerminal,
	Workflow,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { Message, NodeView, PermissionRequest, Session } from "../lib/types";
import { Conversation } from "./conversation";
import { DiffView } from "./diff-view";
import { FileBrowser } from "./file-browser";
import { TerminalPanel } from "./terminal-panel";

type Tab = "conversation" | "diff" | "files" | "terminal";

interface SessionPanelProps {
	node: NodeView | null;
	session: Session | null;
	messages: Message[];
	permissions: PermissionRequest[];
	streamConnected: boolean;
	loadingMessages: boolean;
	sending: boolean;
	interrupting: boolean;
	onSend: (text: string) => Promise<void>;
	onInterrupt: () => Promise<void>;
	onResolvePermission: (
		permission: PermissionRequest,
		decision: "once" | "always" | "reject",
	) => Promise<void>;
	onRefresh: () => void;
	hasOlder: boolean;
	loadingOlder: boolean;
	onLoadOlder: () => Promise<void>;
	/** 手机端单栏模式下的返回按钮；桌面端不传。 */
	onBack?: () => void;
}

const tabs: Array<{ key: Tab; label: string; short: string; icon: typeof MessageSquare }> = [
	{ key: "conversation", label: "Conversation", short: "Chat", icon: MessageSquare },
	{ key: "diff", label: "Diff", short: "Diff", icon: FileDiff },
	{ key: "files", label: "Files", short: "Files", icon: FolderTree },
	{ key: "terminal", label: "Terminal", short: "Terminal", icon: SquareTerminal },
];

export function SessionPanel({
	node,
	session,
	messages,
	permissions,
	streamConnected,
	loadingMessages,
	sending,
	interrupting,
	onSend,
	onInterrupt,
	onResolvePermission,
	onRefresh,
	hasOlder,
	loadingOlder,
	onLoadOlder,
	onBack,
}: SessionPanelProps) {
	const [tab, setTab] = useState<Tab>("conversation");

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset the tab when switching sessions
	useEffect(() => {
		setTab("conversation");
	}, [session?.id]);

	if (!session) {
		return (
			<div className="flex h-full flex-col">
				<div className="flex h-14 items-center border-b px-4">
					<div className="flex items-center gap-2 text-sm text-muted-foreground">
						<Workflow className="size-4" /> Select or create a session
					</div>
				</div>
				<div className="m-auto max-w-md p-6 text-center">
					<div className="mx-auto mb-3 flex size-12 items-center justify-center rounded-2xl bg-muted">
						<MessageSquare className="size-5 text-muted-foreground" />
					</div>
					<h2 className="text-sm font-medium">No session selected</h2>
					<p className="mt-1 text-xs text-muted-foreground">
						Pick a session from the list, or create one with an absolute directory on a connected
						node.
					</p>
				</div>
			</div>
		);
	}

	const directory = session.directory || "";
	const directoryMissing = directory === "";

	return (
		<div className="flex h-full min-h-0 flex-col">
			<header className="shrink-0 border-b px-3 py-2.5 md:px-4 md:py-3">
				<div className="flex items-start gap-2 md:gap-3">
					{onBack ? (
						<Button
							variant="ghost"
							size="icon-sm"
							className="-ml-1 mt-0.5 shrink-0"
							onClick={onBack}
							aria-label="Back to sessions"
						>
							<ChevronLeft className="size-4" />
						</Button>
					) : null}
					<div className="min-w-0 flex-1">
						<div className="flex min-w-0 items-center gap-2">
							<h1 className="min-w-0 truncate text-sm font-semibold">
								{session.title || session.id}
							</h1>
							<Badge
								variant={node?.connected ? "secondary" : "outline"}
								className="max-w-24 shrink-0 truncate md:max-w-40"
							>
								{node?.name ?? "unknown node"}
							</Badge>
						</div>
						<div className="mt-1 flex min-w-0 items-center gap-2 text-[11px] text-muted-foreground">
							<code className="truncate font-mono">{directory || "directory unavailable"}</code>
							<span className="shrink-0">·</span>
							<code className="hidden shrink-0 font-mono sm:inline">{session.id}</code>
						</div>
						{!node?.connected ? (
							<p className="mt-1 text-[11px] font-medium text-destructive">
								Node offline — sending and approvals will fail until it reconnects.
							</p>
						) : null}
					</div>
					<div className="flex shrink-0 items-center gap-1">
						<span
							className={cn(
								"mr-0.5 size-1.5 shrink-0 rounded-full md:mr-1",
								streamConnected ? "bg-emerald-500" : "bg-amber-500",
							)}
							title={
								streamConnected
									? "Live updates connected"
									: "Live updates reconnecting — sending still works"
							}
						/>
						<Button
							variant="outline"
							size="icon-sm"
							className="md:hidden"
							onClick={() => void onInterrupt()}
							disabled={interrupting || !node?.connected}
							aria-label="Interrupt run"
						>
							{interrupting ? (
								<Loader2 className="size-3.5 animate-spin" />
							) : (
								<Square className="size-3 fill-current" />
							)}
						</Button>
						<Button
							variant="outline"
							size="sm"
							className="hidden md:inline-flex"
							onClick={() => void onInterrupt()}
							disabled={interrupting || !node?.connected}
						>
							{interrupting ? "Stopping…" : "Interrupt"}
						</Button>
						<Button variant="ghost" size="icon-sm" onClick={onRefresh} aria-label="Refresh session">
							<RefreshCw className="size-3.5" />
						</Button>
					</div>
				</div>
				<nav className="-mx-1 mt-2 flex gap-1 overflow-x-auto px-1 pb-0.5 md:mt-3">
					{tabs.map((item) => {
						const Icon = item.icon;
						return (
							<button
								type="button"
								key={item.key}
								onClick={() => setTab(item.key)}
								className={cn(
									"inline-flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-2 text-xs font-medium transition-colors md:px-2.5 md:py-1.5",
									tab === item.key
										? "bg-muted text-foreground"
										: "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
								)}
							>
								<Icon className="size-3.5" />
								<span className="md:hidden">{item.short}</span>
								<span className="hidden md:inline">{item.label}</span>
							</button>
						);
					})}
				</nav>
			</header>

			{tab !== "conversation" && directoryMissing ? (
				<div className="m-4 rounded-lg border border-amber-300/70 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-100">
					This session has no directory. Diff, files, and terminal require a session working
					directory.
				</div>
			) : tab === "conversation" ? (
				<Conversation
					messages={messages}
					permissions={permissions}
					onSend={onSend}
					onInterrupt={onInterrupt}
					onResolvePermission={onResolvePermission}
					sending={sending}
					interrupting={interrupting}
					loading={loadingMessages}
					streamConnected={streamConnected}
					nodeConnected={node?.connected ?? false}
					hasOlder={hasOlder}
					loadingOlder={loadingOlder}
					onLoadOlder={onLoadOlder}
				/>
			) : tab === "diff" ? (
				<DiffView nodeID={node?.id ?? ""} sessionID={session.id} directory={directory} />
			) : tab === "files" ? (
				<FileBrowser nodeID={node?.id ?? ""} directory={directory} />
			) : (
				<TerminalPanel nodeID={node?.id ?? ""} sessionID={session.id} directory={directory} />
			)}
		</div>
	);
}
