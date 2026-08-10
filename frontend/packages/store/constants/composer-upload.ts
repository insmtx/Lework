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
	".html",
	".htm",
	".png",
	".jpg",
	".jpeg",
	".gif",
	".bmp",
	".webp",
	".svg",
	".txt",
] as const;

// 原生文件选择器对纯扩展名列表的解析不一致，尤其是 macOS/Electron 下的视频文件。
// MIME 通配符负责让图片可选择，选中后仍由扩展名白名单做精确校验。
export const COMPOSER_UPLOAD_ACCEPT = ["image/*", ...COMPOSER_UPLOAD_ALLOWED_EXTENSIONS].join(",");

export function getNativeFileInputAccept(
	accept: string,
	platform = typeof navigator === "undefined" ? undefined : navigator.platform,
): string | undefined {
	// 银河麒麟的 GTK 文件选择器无法可靠解析 Electron 传入的 accept，
	// 会生成一个空过滤器。Linux 上展示全部文件，具体入口仍在选择后执行类型与大小校验。
	return platform?.toLowerCase().includes("linux") ? undefined : accept;
}

export function getComposerUploadAccept(platform?: string): string | undefined {
	return getNativeFileInputAccept(COMPOSER_UPLOAD_ACCEPT, platform);
}

export const COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE =
	"仅支持上传 PDF、Word、Excel、PPT、Markdown、HTML、图片（JPG/JPEG/PNG/GIF/BMP/WEBP/SVG）、TXT 文件";

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
