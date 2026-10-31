"use client";

import type { BackendProjectFileVersion, ProjectArtifact } from "@lework/store";
import type { Attachment, MessageAttachment } from "@lework/store/types/chat";
import { useSyncExternalStore } from "react";
import {
	artifactToFilePreviewItem,
	type FilePreviewItem,
	toFilePreviewItemFromAttachment,
} from "./file-preview-utils";
import type { ProjectFileNode } from "./project-files";

type FilePreviewSnapshot = {
	file: FilePreviewItem | null;
};

let snapshot: FilePreviewSnapshot = { file: null };
const listeners = new Set<() => void>();

function emit() {
	for (const listener of listeners) {
		listener();
	}
}

function setSnapshot(next: FilePreviewSnapshot) {
	snapshot = next;
	emit();
}

export const filePreviewActions = {
	open(file: FilePreviewItem) {
		setSnapshot({ file });
	},
	close() {
		setSnapshot({ file: null });
	},
	applyLatestProjectFileVersion({
		projectId,
		expectedPublicId,
		version,
		versionCount,
	}: {
		projectId: string;
		expectedPublicId: string;
		version: BackendProjectFileVersion;
		versionCount: number;
	}) {
		const file = snapshot.file;
		if (!file || file.projectId !== projectId || file.publicId !== expectedPublicId) {
			return false;
		}
		setSnapshot({
			file: {
				...file,
				name: version.name || file.name,
				title: version.name || file.name,
				mimeType: version.mime_type || file.mimeType,
				storageUri: version.storage_uri || undefined,
				publicId: version.public_id,
				initialFilePublicId: version.initial_file_public_id,
				versionPublicId: undefined,
				projectPath: version.relative_path || file.projectPath,
				versionLabel: version.version_label,
				versionNo: version.version_no,
				versionCount,
			},
		});
		return true;
	},
};

export function useFilePreviewStore() {
	const state = useSyncExternalStore(
		(onStoreChange) => {
			listeners.add(onStoreChange);
			return () => listeners.delete(onStoreChange);
		},
		() => snapshot,
		() => snapshot,
	);

	return {
		file: state.file,
		isOpen: state.file !== null,
		openFilePreview: filePreviewActions.open,
		closeFilePreview: filePreviewActions.close,
	};
}

export function openProjectFilePreview(
	projectId: string,
	file: ProjectFileNode,
	options?: {
		openHistory?: boolean;
		taskId?: string;
		version?: BackendProjectFileVersion;
	},
) {
	const version = options?.version;
	filePreviewActions.open({
		name: version?.name || file.name,
		title: version?.name || file.name,
		mimeType: version?.mime_type || file.mimeType,
		storageUri: version?.storage_uri || file.storageUri,
		publicId: file.publicId,
		initialFilePublicId: version?.initial_file_public_id || file.initialFilePublicId,
		versionPublicId: version?.public_id,
		projectId,
		projectPath: version?.relative_path || file.path,
		versionLabel: version?.version_label || file.versionLabel,
		versionNo: version?.version_no ?? file.versionNo,
		versionCount: file.versionCount,
		openHistory: options?.openHistory,
		...(options?.taskId ? { taskId: options.taskId } : {}),
	});
}

export function openProjectArtifactPreview(artifact: ProjectArtifact, projectId?: string) {
	filePreviewActions.open(artifactToFilePreviewItem(artifact, projectId));
}

export function openMessageAttachmentPreview(attachment: MessageAttachment) {
	filePreviewActions.open(toFilePreviewItemFromAttachment(attachment));
}

// 中文注释：输入框中的附件可能还未发送，优先使用本地 blob URL，否则使用已上传文件的标识预览。
export function openPendingAttachmentPreview(attachment: Attachment) {
	filePreviewActions.open({
		name: attachment.name,
		title: attachment.name,
		mimeType: attachment.mimeType,
		publicId: attachment.fileUploadId,
		storageUri: attachment.storageUri,
		url: attachment.url,
	});
}

export function openPlanPreview(fileUploadId: string) {
	filePreviewActions.open({
		name: "计划.md",
		title: "计划.md",
		mimeType: "text/markdown",
		publicId: fileUploadId,
	});
}
