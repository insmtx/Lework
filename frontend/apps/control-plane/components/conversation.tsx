"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Textarea } from "@lework/ui/components/ui/textarea";
import { cn } from "@lework/ui/lib/utils";
import { ArrowUp, Loader2, MessageSquare, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { Message, PermissionRequest } from "../lib/types";
import { PermissionCard } from "./permission-card";
import { ToolCardList } from "./tool-card";

interface ConversationProps {
	messages: Message[];
	permissions: PermissionRequest[];
	onSend: (text: string) => Promise<void>;
	onInterrupt: () => Promise<void>;
	onResolvePermission: (
		permission: PermissionRequest,
		decision: "once" | "reject",
	) => Promise<void>;
	sending: boolean;
	interrupting: boolean;
	loading: boolean;
	/** SSE 事件流是否在线；只影响提示文案，不影响能否发送。 */
	streamConnected: boolean;
	/** 节点是否在线决定请求能否成功，是真正的发送开关。 */
	nodeConnected: boolean;
}

function roleLabel(role: string): string {
	if (role === "user") return "You";
	if (role === "assistant") return "Assistant";
	if (role === "idle") return "Run";
	return role;
}

function MessageTurn({ message }: { message: Message }) {
	if (message.role === "user") {
		return (
			<div className="ml-auto max-w-[85%] rounded-2xl rounded-br-sm bg-primary px-4 py-3 text-[15px] text-primary-foreground shadow-sm md:text-sm">
				<div className="mb-1 text-[10px] font-medium uppercase tracking-wider text-primary-foreground/60">
					You
				</div>
				<div className="whitespace-pre-wrap leading-relaxed">{message.text}</div>
			</div>
		);
	}

	if (message.role === "idle") {
		const failed = message.outcome === "error" || Boolean(message.error);
		return (
			<div
				className={cn(
					"flex items-center gap-2 px-2 text-[11px]",
					failed ? "text-destructive" : "text-muted-foreground",
				)}
			>
				<span
					className={cn("size-1.5 rounded-full", failed ? "bg-destructive" : "bg-emerald-500")}
				/>
				{failed ? message.error?.message || "Run failed" : "Run finished"}
				{message.outcome && !failed ? <span className="font-mono">{message.outcome}</span> : null}
			</div>
		);
	}

	return (
		<div className="max-w-[92%] space-y-2">
			<div className="flex items-center gap-2">
				<span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
					{roleLabel(message.role)}
				</span>
				{message.finish ? (
					<span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
						{message.finish}
					</span>
				) : null}
			</div>
			{message.text ? (
				<div className="whitespace-pre-wrap rounded-2xl rounded-tl-sm border bg-card px-4 py-3 text-[15px] leading-relaxed shadow-sm md:text-sm">
					{message.text}
				</div>
			) : null}
			{message.parts?.length ? <ToolCardList parts={message.parts} /> : null}
			{message.error ? (
				<div className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
					{message.error.message}
				</div>
			) : null}
		</div>
	);
}

export function Conversation({
	messages,
	permissions,
	onSend,
	onInterrupt,
	onResolvePermission,
	sending,
	interrupting,
	loading,
	streamConnected,
	nodeConnected,
}: ConversationProps) {
	const [text, setText] = useState("");
	const [error, setError] = useState<string | null>(null);
	const scrollRef = useRef<HTMLDivElement>(null);
	const stickToBottom = useRef(true);

	// biome-ignore lint/correctness/useExhaustiveDependencies: new items should scroll to bottom
	useEffect(() => {
		if (!stickToBottom.current) return;
		const node = scrollRef.current;
		if (node) node.scrollTop = node.scrollHeight;
	}, [messages.length, permissions.length]);

	const submit = async () => {
		const value = text.trim();
		if (!value || sending) return;
		setError(null);
		try {
			await onSend(value);
			setText("");
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "Unable to send prompt");
		}
	};

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div
				ref={scrollRef}
				onScroll={(event) => {
					const element = event.currentTarget;
					stickToBottom.current =
						element.scrollHeight - element.scrollTop - element.clientHeight < 96;
				}}
				className="min-h-0 flex-1 overflow-y-auto px-4 py-4 md:px-6"
			>
				{loading && messages.length === 0 ? (
					<div className="flex items-center gap-2 text-sm text-muted-foreground">
						<Loader2 className="size-4 animate-spin" /> Loading conversation
					</div>
				) : messages.length === 0 ? (
					<div className="mx-auto mt-16 max-w-md text-center">
						<div className="mx-auto mb-3 flex size-12 items-center justify-center rounded-2xl bg-muted">
							<MessageSquare className="size-5 text-muted-foreground" />
						</div>
						<h3 className="text-sm font-medium">No turns yet</h3>
						<p className="mt-1 text-xs text-muted-foreground">
							Describe the task. Lework sends it to OpenCode in this session's directory.
						</p>
					</div>
				) : (
					<div className="mx-auto flex max-w-3xl flex-col gap-4">
						{messages.map((message) => (
							<MessageTurn key={message.id} message={message} />
						))}
					</div>
				)}
			</div>

			{/* 键盘弹出时 composer 需要贴着可视区域底部，并让开 iPhone 的 Home 指示条。 */}
			<div className="border-t bg-background/95 px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-3 md:px-6 md:pb-3">
				<div className="mx-auto max-w-3xl space-y-2">
					{permissions.map((permission) => (
						<PermissionCard
							key={permission.id}
							permission={permission}
							disabled={!nodeConnected}
							onResolve={onResolvePermission}
						/>
					))}
					{error ? (
						<div className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
							{error}
						</div>
					) : null}
					<div className="rounded-xl border bg-card p-2 shadow-sm focus-within:ring-2 focus-within:ring-ring/40">
						<Textarea
							value={text}
							onChange={(event) => setText(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
									event.preventDefault();
									void submit();
								}
							}}
							placeholder="Ask OpenCode to build, explain, or fix something…"
							className="min-h-14 resize-none border-0 bg-transparent px-2 py-1 text-base shadow-none focus-visible:ring-0 md:text-sm"
							disabled={!nodeConnected}
						/>
						<div className="flex items-center justify-between gap-2 px-1">
							<span className="text-[10px] text-muted-foreground">
								{!nodeConnected ? (
									"Node offline — cannot send"
								) : !streamConnected ? (
									"Live updates paused · sending still works"
								) : (
									<span className="hidden md:inline">Enter to send · Shift+Enter for newline</span>
								)}
							</span>
							<div className="flex gap-1.5">
								<Button
									variant="ghost"
									size="sm"
									onClick={() => void onInterrupt()}
									disabled={interrupting}
									title="Interrupt the current run"
								>
									{interrupting ? (
										<Loader2 className="size-3.5 animate-spin" />
									) : (
										<Square className="size-3 fill-current" />
									)}
									<span className="hidden md:inline">Interrupt</span>
								</Button>
								<Button
									size="icon"
									className="size-9 md:size-8"
									onClick={() => void submit()}
									disabled={sending || !text.trim() || !nodeConnected}
									aria-label="Send prompt"
								>
									{sending ? (
										<Loader2 className="size-4 animate-spin" />
									) : (
										<ArrowUp className="size-4" />
									)}
								</Button>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
