import { getFileRelativePath } from "./upload";

export const COMPOSER_UPLOAD_ALLOWED_EXTENSIONS = [
	".pdf",
	".doc",
	".docx",
	".xls",
	".xlsx",
	".ppt",
	".pptx",
	".md",
	".markdown",
	".png",
	".jpg",
	".jpeg",
	".txt",
] as const;

export const COMPOSER_UPLOAD_ACCEPT =
	".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.md,.markdown,.png,.jpg,.jpeg,.txt";

export const COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE =
	"仅支持上传 PDF、Word、Excel、PPT、Markdown、图片（PNG/JPG/JPEG）、TXT 文件";

export const COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE = "不能上传空文件";

export const COMPOSER_UPLOAD_SUCCESS_MESSAGE = "文件上传成功";

export function resolveComposerUploadFileName(file: File): string {
	const relativePath = getFileRelativePath(file);
	return relativePath || file.name;
}

export function getUploadFileExtension(fileName: string): string {
	const normalized = fileName.trim().replace(/\\/g, "/");
	const baseName = normalized.split("/").pop() ?? normalized;
	const dotIndex = baseName.lastIndexOf(".");
	if (dotIndex <= 0 || dotIndex === baseName.length - 1) {
		return "";
	}
	return baseName.slice(dotIndex).toLowerCase();
}

export function isComposerUploadAllowedFileName(fileName: string): boolean {
	const extension = getUploadFileExtension(fileName);
	if (!extension) {
		return false;
	}
	return COMPOSER_UPLOAD_ALLOWED_EXTENSIONS.includes(
		extension as (typeof COMPOSER_UPLOAD_ALLOWED_EXTENSIONS)[number],
	);
}

export function isComposerUploadAllowedFile(file: File): boolean {
	return isComposerUploadAllowedFileName(resolveComposerUploadFileName(file));
}

export function isEmptyUploadFile(file: File): boolean {
	return file.size <= 0;
}

export type ComposerFolderUploadPartition = {
	uploadable: File[];
	skippedEmpty: File[];
	skippedType: File[];
};

export function partitionComposerFolderFiles(files: File[]): ComposerFolderUploadPartition {
	const uploadable: File[] = [];
	const skippedEmpty: File[] = [];
	const skippedType: File[] = [];

	for (const file of files) {
		if (isEmptyUploadFile(file)) {
			skippedEmpty.push(file);
			continue;
		}
		if (!isComposerUploadAllowedFile(file)) {
			skippedType.push(file);
			continue;
		}
		uploadable.push(file);
	}

	return { uploadable, skippedEmpty, skippedType };
}

export function buildComposerFolderUploadSummaryMessage(
	uploadedCount: number,
	skippedEmptyCount: number,
	skippedTypeCount: number,
): string {
	const parts: string[] = [];
	if (uploadedCount > 0) {
		parts.push("文件夹上传成功");
	}
	if (skippedEmptyCount > 0) {
		parts.push(`已跳过 ${skippedEmptyCount} 个空文件`);
	}
	if (skippedTypeCount > 0) {
		parts.push(`已跳过 ${skippedTypeCount} 个不支持的文件类型`);
	}
	if (parts.length === 0) {
		return "文件夹中没有可上传的文件";
	}
	return parts.join("，");
}
