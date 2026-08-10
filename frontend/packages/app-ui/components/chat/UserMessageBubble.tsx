"use client";

import {
	fetchFilePreviewByPublicId,
	formatArtifactTime,
	formatFileSize,
	formatTime,
	useAuthStore,
	useLayoutStore,
} from "@leros/store";
import type { Message, MessageAttachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Check, Copy, Folder, ImageIcon, LoaderCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ProtectedImage } from "../avatar/ProtectedImage";
import { openMessageAttachmentPreview } from "../layout/file-preview-store";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { MessageContentWithComposerTokens } from "./MessageContentWithComposerTokens";

// Button 的 size 只支持预设枚举，这里用受支持的尺寸并通过 className 微调成更紧凑的操作按钮。
const compactActionButtonClassName = "size-[26px]";

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
			className={`${compactActionButtonClassName} ${
				copied ? "text-green-500" : "text-slate-400 hover:text-slate-600"
			}`}
			onClick={handleCopy}
		>
			{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
		</Button>
	);
}

export function UserMessageBubble({
	message,
	projectId,
}: {
	message: Message;
	projectId?: string;
}) {
	const authUser = useAuthStore((state) => state.authUser);
	const projectMembers = useLayoutStore((s) =>
		projectId ? s.projects.find((project) => project.id === projectId)?.members : undefined,
	);
	const isProjectDetailFetching = useLayoutStore((s) =>
		projectId ? s.projectDetailFetchingIds.includes(projectId) : false,
	);
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
	// 中文注释：消息体本身不带头像，按 sender_uin 从项目成员补全真人队友头像。
	const authorMember = useMemo(() => {
		if (isOwnMessage) return undefined;
		const authorId = message.author?.id;
		if (!authorId || !projectMembers?.length) return undefined;
		return projectMembers.find(
			(member) => member.type === "user" && String(member.memberId) === authorId,
		);
	}, [isOwnMessage, message.author?.id, projectMembers]);
	const authorAvatarUrl =
		message.author?.avatarUrl?.trim() || authorMember?.avatarUrl?.trim() || undefined;
	// 中文注释：仅在 DetailProject 请求中且尚未解析到成员/url 时空白占位；已移除成员等确认不在列表后走默认头像。
	const avatarPending =
		!isOwnMessage && !authorAvatarUrl && !authorMember && isProjectDetailFetching;

	return (
		<div
			data-slot="user-message"
			className={`flex min-w-0 w-full items-start gap-2.5 ${isOwnMessage ? "justify-end" : "justify-start"}`}
		>
			{!isOwnMessage && (
				<UserAvatar name={authorName ?? "用户"} src={authorAvatarUrl} pending={avatarPending} />
			)}
			{/* 中文注释：min-w-0 让 max-w 在 flex 布局下真正生效，避免无空格长串把气泡撑出可视区。 */}
			<div
				className={`flex min-w-0 max-w-[78%] flex-col ${isOwnMessage ? "items-end" : "items-start"}`}
			>
				<div
					className={`mb-1.5 flex items-center gap-2 text-xs text-slate-400 ${
						isOwnMessage ? "justify-end" : ""
					}`}
				>
					{!isOwnMessage && authorName && (
						<span className="font-medium text-slate-500">{authorName}</span>
					)}
					{isOwnMessage && authorName && <span>{authorName}</span>}
					{message.status === "sending" && <span className="text-xs text-slate-400">发送中</span>}
					<span>{formatTime(message.timestamp)}</span>
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
						{/* 中文注释：复制按钮与名称/时间一致，常显不依赖悬停。 */}
						<div className={`mt-2 flex ${isOwnMessage ? "justify-end" : "justify-start"}`}>
							<div className="flex items-center gap-0.5">
								<CopyButton text={displayContent} />
							</div>
						</div>
					</>
				)}
			</div>
		</div>
	);
}

function UserAvatar({
	name,
	src,
	pending = false,
}: {
	name: string;
	src?: string;
	pending?: boolean;
}) {
	const frameClassName = "size-8 shrink-0 rounded-full";
	const initial = name.trim().slice(0, 1).toUpperCase() || "U";
	const defaultFallback = (
		<div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-slate-200 to-slate-300 text-xs font-semibold text-slate-600 ring-1 ring-white">
			{initial}
		</div>
	);
	// 中文注释：等成员快照/图片字节到达前用空白占位，交给浏览器原生加载，不做额外渐显。
	const blankPlaceholder = <div className={frameClassName} aria-hidden />;

	// 中文注释：没有头像 url 时才展示默认头像；尚未从 DetailProject 拿到成员信息时保持空白。
	if (!src) {
		return pending ? blankPlaceholder : defaultFallback;
	}

	return (
		<ProtectedImage
			src={src}
			alt={name}
			className={`${frameClassName} object-cover ring-1 ring-white`}
			loadingFallback={blankPlaceholder}
			fallback={defaultFallback}
		/>
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
