"use client";

import {
	fetchFilePreviewByPublicId,
	formatArtifactTime,
	formatFileSize,
	formatTime,
	useAuthStore,
} from "@leros/store";
import type { Message, MessageAttachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Check, Copy, Folder, ImageIcon, LoaderCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { openMessageAttachmentPreview } from "../layout/file-preview-store";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { MessageContentWithComposerTokens } from "./MessageContentWithComposerTokens";

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
	const displayContent = message.metadata?.displayContent ?? message.content;
	const visibleText = displayContent.trim();
	const referenceIds = new Set(
		(message.metadata?.displayComposerTokens ?? message.metadata?.composerTokens ?? [])
			.filter((token) => token.kind === "reference" && token.id)
			.map((token) => token.id as string),
	);
	// 中文注释：选区编辑所附 DOCX 仅用于模型定位，用户侧以引用 token 展示，避免重复出现文件卡片。
	const attachments = (message.attachments ?? []).filter(
		(attachment) => !referenceIds.has(attachment.fileUploadId),
	);
	const currentUserId = authUser?.uin !== undefined ? String(authUser.uin) : undefined;
	// 中文注释：后端落库消息会返回真实 sender_uin，不能只依赖本地 optimistic 的 current-user 标记。
	const isOwnMessage =
		!message.author ||
		message.author.id === "current-user" ||
		(currentUserId !== undefined && message.author.id === currentUserId) ||
		message.author.name === "我";
	// 中文注释：与左下角个人中心一致，优先组织内 uin_name，避免跨组织显示成全局 name。
	const authorName = isOwnMessage
		? (authUser?.uinName ?? authUser?.name ?? message.author?.name)
		: message.author?.name;

	return (
		<div
			data-slot="user-message"
			className={`group flex min-w-0 w-full items-start gap-2.5 ${isOwnMessage ? "justify-end" : "justify-start"}`}
		>
			{!isOwnMessage && <UserAvatar name={authorName ?? "用户"} />}
			{/* 中文注释：min-w-0 让 max-w 在 flex 布局下真正生效，避免无空格长串把气泡撑出可视区。 */}
			<div
				className={`flex min-w-0 max-w-[78%] flex-col ${isOwnMessage ? "items-end" : "items-start"}`}
			>
				<div
					className={`mb-1.5 flex items-center gap-2 text-xs text-slate-400 ${
						isOwnMessage ? "justify-end opacity-0 transition-opacity group-hover:opacity-100" : ""
					}`}
				>
					{!isOwnMessage && authorName && (
						<span className="font-medium text-slate-500">{authorName}</span>
					)}
					{isOwnMessage && authorName && <span>{authorName}</span>}
					{message.status === "sending" && <span className="text-xs text-slate-400">发送中</span>}
					<span>{formatTime(message.timestamp)}</span>
					{!isOwnMessage && visibleText && <CopyButton text={displayContent} />}
				</div>
				{attachments.length > 0 && (
					<div className={`mb-2 flex flex-col gap-2 ${isOwnMessage ? "items-end" : "items-start"}`}>
						{groupMessageAttachmentsForDisplay(attachments).map((item) => {
							if (item.type === "folder") {
								return (
									<FolderAttachmentCard
										key={item.id}
										name={item.name}
										size={item.size}
										createdAt={item.createdAt}
									/>
								);
							}
							const attachment = item.attachment;
							return attachment.mimeType.startsWith("image/") ? (
								<ImageAttachmentCard
									key={attachment.id}
									attachment={attachment}
									onClick={() => openMessageAttachmentPreview(attachment)}
								/>
							) : (
								<FileAttachmentCard
									key={attachment.id}
									attachment={attachment}
									onClick={() => openMessageAttachmentPreview(attachment)}
								/>
							);
						})}
					</div>
				)}
				{visibleText && (
					<>
						<div
							className={`max-w-full min-w-0 w-fit break-words [overflow-wrap:anywhere] rounded-2xl px-4 py-2 text-sm leading-7 text-black shadow-sm ${
								isOwnMessage
									? "rounded-tr-md bg-[#f3f3f4] shadow-blue-600/10"
									: "rounded-tl-md border border-slate-100 bg-white shadow-slate-200/60"
							}`}
						>
							<MessageContentWithComposerTokens message={message} />
						</div>
						{isOwnMessage && (
							<div className="mt-2 flex justify-end">
								<div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
									<CopyButton text={displayContent} />
								</div>
							</div>
						)}
					</>
				)}
			</div>
		</div>
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

function FolderAttachmentCard({
	name,
	size,
	createdAt,
}: {
	name: string;
	size: number;
	createdAt?: number;
}) {
	const meta = [
		size > 0 ? formatFileSize(size) : "",
		createdAt ? formatArtifactTime(createdAt) : "",
	]
		.filter(Boolean)
		.join(" · ");

	return (
		<div className="relative flex w-[260px] min-w-0 items-center gap-3 overflow-hidden rounded-xl border border-slate-200/70 bg-white/90 px-3.5 py-3 text-left shadow-sm">
			<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
				<Folder className="size-5" aria-hidden="true" />
			</div>
			<div className="min-w-0">
				<div className="truncate text-sm font-semibold leading-5 text-[var(--leros-text-strong)]">
					{name}
				</div>
				{meta ? (
					<div className="mt-1 truncate text-xs leading-4 text-[var(--leros-text-muted)]">
						{meta}
					</div>
				) : null}
			</div>
		</div>
	);
}

type DisplayMessageAttachment =
	| { type: "folder"; id: string; name: string; size: number; createdAt?: number }
	| { type: "file"; attachment: MessageAttachment };

function getAttachmentRelativePath(attachment: MessageAttachment): string {
	return attachment.relativePath?.trim() || "";
}

function groupMessageAttachmentsForDisplay(
	attachments: MessageAttachment[],
): DisplayMessageAttachment[] {
	const result: DisplayMessageAttachment[] = [];
	const folderGroups = new Map<string, MessageAttachment[]>();

	for (const attachment of attachments) {
		if (attachment.attachmentType === "folder") {
			result.push({
				type: "folder",
				id: attachment.id,
				name: attachment.name,
				size: attachment.size,
				createdAt: attachment.createdAt,
			});
			continue;
		}

		const relativePath = getAttachmentRelativePath(attachment);
		const slashIndex = relativePath.indexOf("/");
		if (slashIndex <= 0) {
			result.push({ type: "file", attachment });
			continue;
		}

		const folderName = relativePath.slice(0, slashIndex);
		const grouped = folderGroups.get(folderName) ?? [];
		grouped.push(attachment);
		folderGroups.set(folderName, grouped);
	}

	for (const [folderName, grouped] of folderGroups) {
		if (grouped.length >= 1) {
			const totalSize = grouped.reduce((sum, item) => sum + (item.size ?? 0), 0);
			const createdAt = grouped.reduce((latest, item) => Math.max(latest, item.createdAt ?? 0), 0);
			result.push({
				type: "folder",
				id: `folder-${folderName}-${grouped[0]?.id ?? folderName}`,
				name: folderName,
				size: totalSize,
				createdAt: createdAt || undefined,
			});
			continue;
		}
		for (const attachment of grouped) {
			result.push({ type: "file", attachment });
		}
	}

	return result;
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

		// 历史消息中的图片补拉一次 preview 内容生成缩略图，避免只剩文件标识时消息区展示不出来。
		async function loadThumbnail() {
			setThumbnailLoading(true);
			try {
				const response = await fetchFilePreviewByPublicId(attachment.fileUploadId);
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
