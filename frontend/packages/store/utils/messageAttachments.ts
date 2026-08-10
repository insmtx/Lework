import type { Attachment, MessageAttachment } from "../types/chat";

/**
 * 附件预览 URL 统一策略（工作台 / 项目新建 / 任务续聊共用）：
 * - blob/data 只活在输入框本地预览，清空或卸载时必须 revoke；
 * - 写入消息气泡时不带临时地址，缩略图走 fileUploadId（或 https 持久 URL）。
 */

export type OutgoingMessageAttachment = {
	file_upload_id: string;
	name: string;
	mime_type: string;
	size: number;
	relative_path: string;
};

/** 释放输入框本地 blob 预览，避免发送/移除后泄漏 object URL。 */
export function revokeAttachmentObjectUrls(
	attachments: ReadonlyArray<{ url?: string }> | undefined,
): void {
	for (const attachment of attachments ?? []) {
		if (attachment.url?.startsWith("blob:")) {
			URL.revokeObjectURL(attachment.url);
		}
	}
}

export function mapComposerAttachments(
	attachments?: Attachment[],
): MessageAttachment[] | undefined {
	const mapped = attachments
		?.flatMap((attachment) => mapComposerAttachment(attachment))
		.filter((attachment): attachment is MessageAttachment => attachment !== undefined);
	return mapped?.length ? mapped : undefined;
}

function mapComposerAttachment(attachment: Attachment): MessageAttachment[] {
	if (attachment.type === "folder" && attachment.folderFiles?.length) {
		return [
			{
				id: attachment.id,
				fileUploadId: attachment.folderFiles[0]?.fileUploadId ?? attachment.id,
				name: attachment.name,
				mimeType: "application/x-directory",
				size: attachment.size,
				createdAt: Date.now(),
				attachmentType: "folder",
			},
		];
	}

	const fileUploadId = attachment.fileUploadId?.trim();
	if (!fileUploadId) return [];

	return [
		{
			id: attachment.id,
			fileUploadId,
			name: attachment.name,
			mimeType: attachment.mimeType || attachment.file?.type || "application/octet-stream",
			size: attachment.size,
			relativePath: attachment.name.trim(),
			createdAt: Date.now(),
			url: durableAttachmentUrl(attachment.url),
			storageUri: attachment.storageUri,
		},
	];
}

/** 过滤输入框临时预览地址，避免乐观消息绑定已被 revoke 的 blob URL。 */
function durableAttachmentUrl(url?: string): string | undefined {
	const trimmed = url?.trim();
	if (!trimmed) return undefined;
	if (trimmed.startsWith("blob:") || trimmed.startsWith("data:")) return undefined;
	return trimmed;
}

export function mapOutgoingAttachments(
	attachments?: Attachment[],
): OutgoingMessageAttachment[] | undefined {
	const mapped: OutgoingMessageAttachment[] = [];

	for (const attachment of attachments ?? []) {
		if (attachment.type === "folder" && attachment.folderFiles?.length) {
			for (const file of attachment.folderFiles) {
				const fileUploadId = file.fileUploadId.trim();
				if (!fileUploadId) continue;
				const fileName = file.name.trim();
				mapped.push({
					file_upload_id: fileUploadId,
					name: fileName,
					mime_type: file.mimeType || "application/octet-stream",
					size: file.size,
					relative_path: file.relativePath?.trim() || fileName,
				});
			}
			continue;
		}

		const fileUploadId = attachment.fileUploadId?.trim();
		if (!fileUploadId) continue;
		const fileName = attachment.name.trim();
		mapped.push({
			file_upload_id: fileUploadId,
			name: fileName,
			mime_type: attachment.mimeType || attachment.file?.type || "application/octet-stream",
			size: attachment.size,
			relative_path: fileName,
		});
	}

	return mapped.length ? mapped : undefined;
}
