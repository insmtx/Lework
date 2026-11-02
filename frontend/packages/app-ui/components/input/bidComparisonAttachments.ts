import { projectFileApi } from "@lework/store";
import type { Attachment } from "@lework/store/types/chat";
import type { BidComparisonConfig, BidComparisonProjectFile } from "./BidComparisonConfigDialog";

type UploadBidFileResult = {
	publicId: string;
	storageUri?: string;
	mimeType?: string;
	size?: number;
	originalName?: string;
};

async function uploadBidLocalFile(
	file: File,
	projectId?: string | null,
): Promise<UploadBidFileResult> {
	const response = projectId
		? await projectFileApi.upload({
				projectId,
				projectPublicId: projectId,
				file,
			})
		: await projectFileApi.uploadLoose({
				file,
				purpose: "attachment",
				withLocalPath: true,
			});
	const payload = response.data;
	const publicId = payload.public_id?.trim();
	if (!publicId) {
		throw new Error(`文件「${file.name}」上传失败：缺少文件 ID`);
	}
	return {
		publicId,
		storageUri: payload.storage_uri,
		mimeType: payload.mime_type || file.type,
		size: payload.file_size ?? payload.size ?? file.size,
		originalName: payload.original_name || payload.filename || file.name,
	};
}

async function resolveBidFile(
	file: BidComparisonProjectFile,
	projectId?: string | null,
): Promise<BidComparisonProjectFile> {
	const existingPublicId = file.publicId?.trim();
	if (existingPublicId) {
		return { ...file, publicId: existingPublicId, file: undefined };
	}
	if (!file.file) {
		throw new Error(`文件「${file.name}」缺少可上传内容，请重新选择`);
	}
	const uploaded = await uploadBidLocalFile(file.file, projectId);
	return {
		...file,
		name: uploaded.originalName || file.name,
		publicId: uploaded.publicId,
		storageUri: uploaded.storageUri || file.storageUri,
		mimeType: uploaded.mimeType || file.mimeType,
		size: uploaded.size ?? file.size,
		file: undefined,
	};
}

/** 中文注释：开始对比前把本地未上传文件落到 file_upload_id，便于发送时携带附件角色。 */
export async function ensureBidComparisonFilesUploaded(
	config: BidComparisonConfig,
	projectId?: string | null,
): Promise<BidComparisonConfig> {
	const mainFile = config.mainFile
		? await resolveBidFile(config.mainFile, projectId)
		: undefined;
	const compareFiles: BidComparisonProjectFile[] = [];
	for (const file of config.compareFiles) {
		compareFiles.push(await resolveBidFile(file, projectId));
	}
	return {
		...config,
		mainFile,
		compareFiles,
	};
}

function toAttachment(
	file: BidComparisonProjectFile,
	attachmentRole: "main" | "compare",
): Attachment {
	const fileUploadId = file.publicId?.trim();
	if (!fileUploadId) {
		throw new Error(`文件「${file.name}」尚未上传完成`);
	}
	return {
		id: `bid-${attachmentRole}-${fileUploadId}`,
		type: file.mimeType?.startsWith("image/") ? "image" : "file",
		name: file.name,
		size: file.size ?? 0,
		url: file.previewUrl,
		fileUploadId,
		mimeType: file.mimeType,
		storageUri: file.storageUri,
		uploadStatus: "completed",
		attachmentRole,
	};
}

/** 中文注释：把标书对比配置转成带附件角色的列表，再与输入框普通附件合并发送。 */
export function bidComparisonConfigToAttachments(config: BidComparisonConfig): Attachment[] {
	const attachments: Attachment[] = [];
	if (config.mainFile) {
		attachments.push(toAttachment(config.mainFile, "main"));
	}
	for (const file of config.compareFiles) {
		attachments.push(toAttachment(file, "compare"));
	}
	return attachments;
}

/** 中文注释：标书对比直接发送时使用固定任务说明，不回填或覆盖用户输入框内容。 */
export function bidComparisonPrompt(config: BidComparisonConfig): string {
	return [
		"请进行标书对比分析。",
		`投标文件：${config.mainFile?.name ?? "未指定"}`,
		`对比文件：${config.compareFiles.map((file) => file.name).join("、") || "未指定"}`,
		`报告格式：${config.reportFormat}`,
		config.comparisonRequirements.trim()
			? `对比要求：${config.comparisonRequirements.trim()}`
			: "",
		"请重点总结文件差异、风险点、评分影响和可执行的修改建议。",
	]
		.filter(Boolean)
		.join("\n");
}

/** 将展示层报告格式映射为 API 稳定格式值。 */
export function bidComparisonOutputFormat(
	config: BidComparisonConfig | undefined,
): "docx" | "pdf" | "pptx" | "md" | undefined {
	if (!config) return undefined;
	switch (config.reportFormat) {
		case "Word":
			return "docx";
		case "PDF":
			return "pdf";
		case "PPT":
			return "pptx";
		case "MD":
			return "md";
	}
}
