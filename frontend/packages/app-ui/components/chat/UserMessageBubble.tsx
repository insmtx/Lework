"use client";

import {
	fetchFileDownload,
	formatArtifactTime,
	formatFileSize,
	formatTime,
	useAuthStore,
} from "@leros/store";
import type { Message, MessageAttachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Check, Copy, ImageIcon, LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { SkillDirectiveBadge } from "../common/SkillDirectiveBadge";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { MessageAttachmentPreviewDialog } from "./MessageAttachmentPreviewDialog";

function CopyButton({ text }: { text: string }) {
	const [copied, setCopied] = useState(false);
	const handleCopy = () => {
		navigator.clipboard.writeText(text);
		setCopied(true);
		setTimeout(() => setCopied(false), 1500);
	};
	return (
		<Button
			variant="ghost"
			size="icon-xs"
			className={copied ? "text-green-400" : "text-slate-300 hover:text-slate-400"}
			onClick={handleCopy}
		>
			{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
		</Button>
	);
}

export function UserMessageBubble({ message }: { message: Message }) {
	const authUser = useAuthStore((state) => state.authUser);
	const [previewAttachment, setPreviewAttachment] = useState<MessageAttachment | null>(null);
	const visibleText = message.content.trim();
	const attachments = message.attachments ?? [];
	const currentUserId = authUser?.uin !== undefined ? String(authUser.uin) : undefined;
	// 中文注释：后端落库消息会返回真实 sender_uin，不能只依赖本地 optimistic 的 current-user 标记。
	const isOwnMessage =
		!message.author ||
		message.author.id === "current-user" ||
		(currentUserId !== undefined && message.author.id === currentUserId) ||
		message.author.name === "我";
	const authorName = isOwnMessage ? (authUser?.name ?? message.author?.name) : message.author?.name;

	return (
		<>
			<div
				data-slot="user-message"
				className={`group flex items-start gap-2.5 ${isOwnMessage ? "justify-end" : "justify-start"}`}
			>
				{!isOwnMessage && <UserAvatar name={authorName ?? "用户"} />}
				<div className={`flex max-w-[78%] flex-col ${isOwnMessage ? "items-end" : "items-start"}`}>
					<div
						className={`mb-1.5 flex items-center gap-2 text-xs text-slate-400 ${
							isOwnMessage ? "justify-end opacity-0 transition-opacity group-hover:opacity-100" : ""
						}`}
					>
						{!isOwnMessage && authorName && (
							<span className="font-medium text-slate-500">{authorName}</span>
						)}
						{isOwnMessage && visibleText && <CopyButton text={message.content} />}
						{isOwnMessage && authorName && <span>{authorName}</span>}
						{message.status === "sending" && <span className="text-xs text-slate-400">发送中</span>}
						<span>{formatTime(message.timestamp)}</span>
						{!isOwnMessage && visibleText && <CopyButton text={message.content} />}
					</div>
					{attachments.length > 0 && (
						<div
							className={`mb-2 flex flex-col gap-2 ${isOwnMessage ? "items-end" : "items-start"}`}
						>
							{attachments.map((attachment) =>
								attachment.mimeType.startsWith("image/") ? (
									<ImageAttachmentCard
										key={attachment.id}
										attachment={attachment}
										onClick={() => setPreviewAttachment(attachment)}
									/>
								) : (
									<FileAttachmentCard
										key={attachment.id}
										attachment={attachment}
										onClick={() => setPreviewAttachment(attachment)}
									/>
								),
							)}
						</div>
					)}
					{visibleText && (
						<div
							className={`w-fit rounded-2xl px-4 py-2 text-sm leading-7 text-black shadow-sm ${
								isOwnMessage
									? "rounded-tr-md bg-[#f3f3f4] shadow-blue-600/10"
									: "rounded-tl-md border border-slate-100 bg-white shadow-slate-200/60"
							}`}
						>
							<MessageContentWithComposerTokens message={message} />
						</div>
					)}
				</div>
			</div>
			<MessageAttachmentPreviewDialog
				attachment={previewAttachment}
				open={previewAttachment !== null}
				onOpenChange={(open) => {
					if (!open) setPreviewAttachment(null);
				}}
			/>
		</>
	);
}

function UserAvatar({ name }: { name: string }) {
	const initial = name.trim().slice(0, 1).toUpperCase() || "U";
	return (
		<div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-slate-200 to-slate-300 text-xs font-semibold text-slate-600 ring-1 ring-white">
			{initial}
		</div>
	);
}

function MessageContentWithComposerTokens({ message }: { message: Message }) {
	const tokens = (message.metadata?.composerTokens ?? [])
		.filter((token) => message.content.slice(token.start, token.end) === token.label)
		.sort((a, b) => a.start - b.start);

	if (tokens.length === 0) {
		// 中文注释：没有明确 token metadata 时，普通内容里的 @ 和 / 必须原样展示，不能靠文本猜样式。
		return <span className="whitespace-pre-wrap break-words">{message.content}</span>;
	}

	const parts: React.ReactNode[] = [];
	let cursor = 0;
	tokens.forEach((token, index) => {
		if (token.start > cursor) {
			parts.push(
				<span key={`text-${index}`} className="whitespace-pre-wrap break-words">
					{message.content.slice(cursor, token.start)}
				</span>,
			);
		}
		parts.push(
			token.kind === "skill" ? (
				<SkillDirectiveBadge
					key={`token-${index}`}
					name={token.label.replace(/^\/+/, "")}
					variant="on-blue"
				/>
			) : (
				<span
					key={`token-${index}`}
					className="inline-flex max-w-full items-center rounded-md bg-blue-100 px-1.5 py-0.5 text-xs font-medium leading-none text-blue-700"
				>
					{token.label}
				</span>
			),
		);
		cursor = token.end;
	});

	if (cursor < message.content.length) {
		parts.push(
			<span key="text-tail" className="whitespace-pre-wrap break-words">
				{message.content.slice(cursor)}
			</span>,
		);
	}

	return <div className="flex flex-wrap items-center gap-1.5">{parts}</div>;
}

function ImageAttachmentCard({
	attachment,
	onClick,
}: {
	attachment: MessageAttachment;
	onClick: () => void;
}) {
	const [thumbnailUrl, setThumbnailUrl] = useState<string | null>(
		isInlinePreviewableUrl(attachment.url) ? (attachment.url ?? null) : null,
	);
	const [thumbnailLoading, setThumbnailLoading] = useState(false);
	const metaText = [
		formatFileSize(attachment.size),
		attachment.createdAt ? formatArtifactTime(attachment.createdAt) : "",
	]
		.filter(Boolean)
		.join(" · ");

	useEffect(() => {
		if (attachment.url && isInlinePreviewableUrl(attachment.url)) {
			setThumbnailUrl(attachment.url);
			return;
		}
		if (!attachment.fileUploadId) {
			setThumbnailUrl(null);
			return;
		}

		let cancelled = false;
		let objectUrl: string | null = null;

		// 历史消息中的图片补拉一次 blob 生成缩略图，避免只剩存储路径时消息区展示不出来。
		async function loadThumbnail() {
			setThumbnailLoading(true);
			try {
				const response = await fetchFileDownload(attachment.fileUploadId);
				const blob = await response.blob();
				objectUrl = URL.createObjectURL(blob);
				if (!cancelled) setThumbnailUrl(objectUrl);
			} catch (error) {
				if (!cancelled) {
					console.error("Load user attachment thumbnail error:", error);
					setThumbnailUrl(null);
				}
			} finally {
				if (!cancelled) setThumbnailLoading(false);
			}
		}

		void loadThumbnail();

		return () => {
			cancelled = true;
			if (objectUrl) URL.revokeObjectURL(objectUrl);
		};
	}, [attachment.fileUploadId, attachment.url]);

	return (
		<button
			type="button"
			data-file-preview-trigger
			onClick={onClick}
			className="group/attachment relative flex w-[260px] min-w-0 items-center gap-3 overflow-hidden rounded-xl border border-slate-200/70 bg-white/90 px-3.5 py-3 text-left shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50/60"
			title={attachment.name}
		>
			<div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-[var(--leros-primary-softer)] text-slate-400">
				{thumbnailUrl ? (
					<img src={thumbnailUrl} alt={attachment.name} className="h-full w-full object-cover" />
				) : thumbnailLoading ? (
					<LoaderCircle className="size-5 animate-spin" />
				) : (
					<ImageIcon className="size-5" />
				)}
			</div>
			<div className="min-w-0">
				<div className="truncate text-sm font-normal leading-5 text-[var(--leros-text-strong)]">
					{attachment.name}
				</div>
				{metaText ? (
					<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
						{metaText}
					</div>
				) : null}
			</div>
			<AttachmentHoverMask />
		</button>
	);
}

function FileAttachmentCard({
	attachment,
	onClick,
}: {
	attachment: MessageAttachment;
	onClick: () => void;
}) {
	const metaText = [
		formatFileSize(attachment.size),
		attachment.createdAt ? formatArtifactTime(attachment.createdAt) : "",
	]
		.filter(Boolean)
		.join(" · ");

	return (
		<button
			type="button"
			data-file-preview-trigger
			onClick={onClick}
			className="group/attachment relative flex w-[260px] min-w-0 items-center gap-3 overflow-hidden rounded-xl border border-slate-200/70 bg-white/90 px-3.5 py-3 text-left shadow-sm transition-colors hover:border-blue-200 hover:bg-blue-50/60"
			title={attachment.name}
		>
			<AttachmentHoverMask />
			<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)]">
				<ProjectFileTypeIcon fileName={attachment.name} />
			</div>
			<div className="min-w-0">
				<div className="truncate text-sm font-normal leading-5 text-[var(--leros-text-strong)]">
					{attachment.name}
				</div>
				{metaText ? (
					<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
						{metaText}
					</div>
				) : null}
			</div>
		</button>
	);
}

function AttachmentHoverMask() {
	return (
		<div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-[rgba(15,23,42,0.16)] opacity-0 transition-opacity duration-200 group-hover/attachment:opacity-100">
			<span className="rounded-full bg-[rgba(15,23,42,0.72)] px-3 py-1 text-xs font-medium tracking-[0.02em] text-white shadow-sm">
				点击预览
			</span>
		</div>
	);
}

function isInlinePreviewableUrl(url?: string): boolean {
	if (!url) return false;
	return url.startsWith("blob:") || url.startsWith("data:") || /^https?:\/\//.test(url);
}
