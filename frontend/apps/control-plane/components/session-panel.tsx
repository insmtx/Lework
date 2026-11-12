"use client";

import { Badge } from "@lework/ui/components/ui/badge";
import { Button } from "@lework/ui/components/ui/button";
import { cn } from "@lework/ui/lib/utils";
import {
	FileDiff,
	FolderTree,
	MessageSquare,
	RefreshCw,
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
	connected: boolean;
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
}

const tabs: Array<{ key: Tab; label: string; icon: typeof MessageSquare }> = [
	{ key: "conversation", label: "Conversation", icon: MessageSquare },
	{ key: "diff", label: "Diff", icon: FileDiff },
	{ key: "files", label: "Files", icon: FolderTree },
	{ key: "terminal", label: "Terminal", icon: SquareTerminal },
];

export function SessionPanel({
	node,
	session,
	messages,
	permissions,
	connected,
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
						Pick a session on the left, or create one with an absolute directory on a connected
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
			<header className="border-b px-4 py-3">
				<div className="flex items-start gap-3">
					<div className="min-w-0 flex-1">
						<div className="flex items-center gap-2">
							<h1 className="truncate text-sm font-semibold">{session.title || session.id}</h1>
							<Badge variant={node?.connected ? "secondary" : "outline"}>
								{node?.name ?? "unknown node"}
							</Badge>
						</div>
						<div className="mt-1 flex min-w-0 items-center gap-2 text-[11px] text-muted-foreground">
							<code className="truncate font-mono">{directory || "directory unavailable"}</code>
							<span className="shrink-0">·</span>
							<code className="shrink-0 font-mono">{session.id}</code>
						</div>
					</div>
					<div className="flex shrink-0 items-center gap-1">
						<Button
							variant="outline"
							size="sm"
							onClick={() => void onInterrupt()}
							disabled={interrupting}
						>
							{interrupting ? "Stopping…" : "Interrupt"}
						</Button>
						<span
							className={cn(
								"mr-1 size-1.5 rounded-full",
								connected ? "bg-emerald-500" : "bg-muted-foreground/40",
							)}
							title={connected ? "Event stream connected" : "Event stream reconnecting"}
						/>
						<Button variant="ghost" size="sm" onClick={onRefresh}>
							<RefreshCw className="size-3.5" /> Refresh
						</Button>
					</div>
				</div>
				<nav className="mt-3 flex gap-1 overflow-x-auto">
					{tabs.map((item) => {
						const Icon = item.icon;
						return (
							<button
								type="button"
								key={item.key}
								onClick={() => setTab(item.key)}
								className={cn(
									"inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium transition-colors",
									tab === item.key
										? "bg-muted text-foreground"
										: "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
								)}
							>
								<Icon className="size-3.5" />
								{item.label}
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
					connected={connected}
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
