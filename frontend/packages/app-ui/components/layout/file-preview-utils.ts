import {
	fetchFilePreviewByPublicId,
	fetchFilePreviewByStorageUri,
	projectFileApi,
} from "@leros/store";
import { getOfficeOpenXmlFormat, type OfficeOpenXmlFormat } from "./OfficePreview";

export const FILE_PREVIEW_DRAWER_DEFAULT_WIDTH = 860;
export const FILE_PREVIEW_DRAWER_MIN_WIDTH = 720;
export const FILE_PREVIEW_DRAWER_MAX_WIDTH = 1200;
export const PROJECT_FILE_VERSION_CHANGED_EVENT = "leros:project-file-version-changed";

export type FilePreviewKind =
	| OfficeOpenXmlFormat
	| "spreadsheet"
	| "markdown"
	| "html"
	| "text"
	| "image"
	| "video"
	| "pdf"
	| "unsupported";

const PREVIEW_IMAGE_EXTENSIONS = [".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"];
const PREVIEW_VIDEO_EXTENSIONS = [".mp4", ".mov", ".avi"];

function hasPreviewExtension(name: string, extensions: string[]): boolean {
	return extensions.some((extension) => name.endsWith(extension));
}

export type FilePreviewItem = {
	name: string;
	title?: string;
	mimeType?: string;
	storageUri?: string;
	publicId?: string;
	initialFilePublicId?: string;
	versionPublicId?: string;
	projectId?: string;
	projectPath?: string;
	versionLabel?: string;
	versionNo?: number;
	versionCount?: number;
	taskId?: string;
	openHistory?: boolean;
	url?: string;
};

export type FilePreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "ready"; text?: string; objectUrl?: string; buffer?: ArrayBuffer; mimeType?: string }
	| { status: "error"; message: string };

export function detectFilePreviewKind(item: FilePreviewItem | null): FilePreviewKind {
	if (!item) return "unsupported";

	const mimeType = item.mimeType?.toLowerCase() ?? "";
	const name = item.name.toLowerCase();
	const officeFormat = getOfficeOpenXmlFormat(name, mimeType);

	if (officeFormat) return officeFormat;
	if (
		mimeType.includes("spreadsheet") ||
		mimeType.includes("excel") ||
		mimeType === "text/csv" ||
		name.endsWith(".xls") ||
		name.endsWith(".csv")
	) {
		return "spreadsheet";
	}
	if (mimeType.includes("markdown") || name.endsWith(".md") || name.endsWith(".markdown")) {
		return "markdown";
	}
	if (mimeType.includes("html") || name.endsWith(".html") || name.endsWith(".htm")) {
		return "html";
	}
	if (mimeType.startsWith("image/") || hasPreviewExtension(name, PREVIEW_IMAGE_EXTENSIONS)) {
		return "image";
	}
	if (mimeType.startsWith("video/") || hasPreviewExtension(name, PREVIEW_VIDEO_EXTENSIONS)) {
		return "video";
	}
	if (mimeType === "application/pdf" || mimeType.includes("pdf") || name.endsWith(".pdf")) {
		return "pdf";
	}
	if (
		mimeType.startsWith("text/") ||
		mimeType.includes("json") ||
		mimeType.includes("javascript") ||
		mimeType.includes("typescript") ||
		name.endsWith(".txt") ||
		name.endsWith(".json") ||
		name.endsWith(".yaml") ||
		name.endsWith(".yml") ||
		name.endsWith(".log")
	) {
		return "text";
	}

	return "unsupported";
}

export async function fetchFilePreviewContent(
	item: FilePreviewItem,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	if (item.projectId && item.versionPublicId) {
		return projectFileApi.fetchDownloadVersion(item.projectId, item.versionPublicId, options);
	}
	if (item.projectId && item.publicId) {
		return projectFileApi.fetchDownloadVersion(item.projectId, item.publicId, options);
	}
	if (item.storageUri) {
		return fetchFilePreviewByStorageUri(item.storageUri, options);
	}
	if (item.projectId && item.projectPath) {
		return projectFileApi.fetchDownload(item.projectId, item.projectPath, options);
	}
	if (item.publicId) {
		return fetchFilePreviewByPublicId(item.publicId, options);
	}
	if (item.url?.startsWith("blob:") || item.url) {
		return fetch(item.url, { signal: options?.signal });
	}
	throw new Error("文件缺少预览来源");
}

export async function downloadFilePreviewContent(item: FilePreviewItem): Promise<Response> {
	return fetchFilePreviewContent(item);
}

export function toFilePreviewItemFromAttachment(attachment: {
	name: string;
	mimeType: string;
	storageUri?: string;
	fileUploadId?: string;
	url?: string;
}): FilePreviewItem {
	return {
		name: attachment.name,
		title: attachment.name,
		mimeType: attachment.mimeType,
		storageUri: attachment.storageUri,
		publicId: attachment.fileUploadId,
		url: attachment.url,
	};
}

export type ArtifactPreviewItem = {
	id: string;
	name: string;
	title: string;
	description?: string;
	type: "document" | "spreadsheet" | "image";
	artifactType: string;
	mimeType?: string;
	size: string;
	updatedAt?: number;
	downloadUrl: string;
	storageUri?: string;
	sha256?: string;
};

export function artifactToFilePreviewItem(
	artifact: ArtifactPreviewItem,
	projectId?: string,
): FilePreviewItem {
	const hasProjectPath = Boolean(projectId && artifact.id);

	return {
		name: artifact.name,
		title: artifact.title || artifact.name,
		mimeType: artifact.mimeType,
		storageUri: artifact.storageUri,
		publicId: artifact.storageUri ? undefined : artifact.id,
		projectId: hasProjectPath ? projectId : undefined,
		projectPath: hasProjectPath ? artifact.id : undefined,
	};
}
