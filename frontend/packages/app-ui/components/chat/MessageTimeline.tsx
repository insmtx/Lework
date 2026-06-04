"use client";

import { useChatStore } from "@leros/store";
import type { Message } from "@leros/store/types/chat";
import { cn } from "@leros/ui/lib/utils";
import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
import { AIMessageBubble } from "./AIMessageBubble";
import { TypingIndicator } from "./TypingIndicator";
import { UserMessageBubble } from "./UserMessageBubble";
import { WelcomeScreen } from "./WelcomeScreen";

function formatTime(timestamp: number): string {
	const date = new Date(timestamp);
	return date.toLocaleTimeString("zh-CN", {
		hour: "2-digit",
		minute: "2-digit",
	});
}

export function MessageTimeline({
	emptyState,
	className,
	contentClassName,
}: {
	emptyState?: ReactNode;
	className?: string;
	contentClassName?: string;
} = {}) {
	const { messagesMap, messageIds, isGenerating, streamingMessageId } = useChatStore((s) => s);

	const scrollRef = useRef<HTMLDivElement>(null);
	const prevMessageCountRef = useRef(0);
	const prevStreamSignatureRef = useRef("");

	const messages = messageIds
		.map((id) => messagesMap[id])
		.filter((m): m is Message => m !== undefined);

	useEffect(() => {
		const container = scrollRef.current;
		if (!container) return;

		const nearBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 120;

		const messageCountIncreased = messages.length > prevMessageCountRef.current;
		prevMessageCountRef.current = messages.length;

		const streamingMsg = streamingMessageId ? messagesMap[streamingMessageId] : null;
		const streamSignature = streamingMsg
			? [
					streamingMsg.content,
					streamingMsg.thinking,
					streamingMsg.toolCalls?.map((toolCall) => `${toolCall.id}:${toolCall.status}`).join("|"),
					streamingMsg.todos?.map((todo) => `${todo.id}:${todo.status}`).join("|"),
					streamingMsg.approvals
						?.map((approval) => `${approval.requestId}:${approval.status}`)
						.join("|"),
					streamingMsg.artifacts?.map((artifact) => artifact.id).join("|"),
				].join("\n")
			: "";
		const streamChanged = streamSignature !== prevStreamSignatureRef.current;
		prevStreamSignatureRef.current = streamSignature;

		if (nearBottom || messageCountIncreased || streamChanged) {
			container.scrollTop = container.scrollHeight;
		}
	}, [messages.length, streamingMessageId, messagesMap]);

	const isEmpty = messages.length === 0 && !isGenerating;

	return (
		<div
			ref={scrollRef}
			data-slot="message-timeline"
			className={cn("min-h-0 flex-1 overflow-y-auto", className)}
		>
			{isEmpty ? (
				(emptyState ?? <WelcomeScreen />)
			) : (
				<div
					className={cn(
						"mx-auto flex w-full max-w-[1040px] flex-col gap-4 px-5 py-5 sm:px-6 lg:px-8",
						contentClassName,
					)}
				>
					{messages.length > 0 && (
						<div className="flex items-center justify-center py-1">
							<span className="rounded-full bg-white/70 px-3 py-1 text-xs text-slate-400 shadow-sm ring-1 ring-slate-200/50">
								{formatTime(messages[0]?.timestamp ?? 0)}
							</span>
						</div>
					)}
					{messages.map((msg: Message) => (
						<div key={msg.id}>
							{msg.role === "user" ? (
								<UserMessageBubble message={msg} />
							) : msg.role === "assistant" ? (
								<AIMessageBubble message={msg} isStreaming={msg.id === streamingMessageId} />
							) : null}
						</div>
					))}
					{isGenerating && !streamingMessageId && <TypingIndicator />}
				</div>
			)}
		</div>
	);
}
