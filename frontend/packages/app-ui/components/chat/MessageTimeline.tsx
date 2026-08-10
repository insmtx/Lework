"use client";

import { useChatStore } from "@leros/store";
import type { Message } from "@leros/store/types/chat";
import { cn } from "@leros/ui/lib/utils";
import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
import { AIMessageBubble } from "./AIMessageBubble";
import { TypingIndicator } from "./TypingIndicator";
import { UserMessageBubble } from "./UserMessageBubble";

export function MessageTimeline({
	emptyState,
	className,
	contentClassName,
	contentShellClassName,
	projectId,
}: {
	emptyState?: ReactNode;
	className?: string;
	contentClassName?: string;
	/** 与 contentClassName 配合：外层 shell 负责 padding，内层负责 max-width */
	contentShellClassName?: string;
	projectId?: string;
} = {}) {
	const { messagesMap, messageIds, isGenerating, streamingMessageId } = useChatStore((s) => s);

	const scrollRef = useRef<HTMLDivElement>(null);
	const prevMessageCountRef = useRef(0);
	const prevStreamSignatureRef = useRef("");
	const autoFollowRef = useRef(true);

	const messages = messageIds
		.map((id) => messagesMap[id])
		.filter((m): m is Message => m !== undefined);

	useEffect(() => {
		const container = scrollRef.current;
		if (!container) return;

		const messageCountIncreased = messages.length > prevMessageCountRef.current;
		prevMessageCountRef.current = messages.length;

		const streamingMessages = messages.filter(
			(message) => message.id === streamingMessageId || message.status === "streaming",
		);
		const streamSignature = streamingMessages
			.map((streamingMsg) =>
				[
					streamingMsg.id,
					streamingMsg.content,
					streamingMsg.processSteps
						?.map((step) =>
							step.type === "thinking" ? `thinking:${step.content}` : `tool:${step.toolCallId}`,
						)
						.join("|"),
					streamingMsg.toolCalls?.map((toolCall) => `${toolCall.id}:${toolCall.status}`).join("|"),
					streamingMsg.todos?.map((todo) => `${todo.id}:${todo.status}`).join("|"),
					streamingMsg.approvals
						?.map((approval) => `${approval.requestId}:${approval.status}`)
						.join("|"),
					streamingMsg.artifacts?.map((artifact) => artifact.id).join("|"),
				].join("\n"),
			)
			.join("\n---\n");
		const streamChanged = streamSignature !== prevStreamSignatureRef.current;
		prevStreamSignatureRef.current = streamSignature;

		// 仅在仍然处于“跟随最新”模式时自动贴底，避免用户上滑查看历史时被强制拉回。
		if (autoFollowRef.current && (messageCountIncreased || streamChanged)) {
			container.scrollTop = container.scrollHeight;
		}
	}, [messages.length, streamingMessageId, messagesMap]);

	const isEmpty = messages.length === 0 && !isGenerating;

	const messageList = (
		<>
			{messages.map((msg: Message) => (
				<div key={msg.id} className="min-w-0 py-0.5">
					{msg.role === "user" ? (
						<UserMessageBubble message={msg} projectId={projectId} />
					) : msg.role === "assistant" ? (
						<AIMessageBubble
							message={msg}
							isStreaming={msg.id === streamingMessageId || msg.status === "streaming"}
							projectId={projectId}
						/>
					) : null}
				</div>
			))}
			{isGenerating && !streamingMessageId && <TypingIndicator />}
		</>
	);

	return (
		<div
			ref={scrollRef}
			data-slot="message-timeline"
			onWheel={(event) => {
				// 鼠标滚轮步进小：向上滚时立刻退出跟随，避免等 onScroll 时仍在底部附近又被重新打开。
				if (event.deltaY < 0) {
					autoFollowRef.current = false;
				}
			}}
			onScroll={(event) => {
				const container = event.currentTarget;
				const distanceToBottom =
					container.scrollHeight - container.scrollTop - container.clientHeight;

				// 滞回：贴底附近（≤120）不因小幅上滚重新打开跟随；真正回到底部（≤4）才恢复。
				if (distanceToBottom <= 4) {
					autoFollowRef.current = true;
				} else if (distanceToBottom > 120) {
					autoFollowRef.current = false;
				}
			}}
			className={cn("no-scrollbar min-h-0 flex-1 overflow-y-auto", className)}
		>
			{isEmpty ? (
				(emptyState ?? null)
			) : contentShellClassName ? (
				<div className={contentShellClassName}>
					<div className={cn("flex w-full flex-col gap-3", contentClassName)}>{messageList}</div>
				</div>
			) : (
				<div
					className={cn(
						"mx-auto flex w-full max-w-[1040px] flex-col gap-3 px-5 py-5 sm:px-6 lg:px-8",
						contentClassName,
					)}
				>
					{messageList}
				</div>
			)}
		</div>
	);
}
