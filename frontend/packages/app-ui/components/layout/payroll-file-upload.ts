import { projectFileApi } from "@lework/store";
import type { Attachment } from "@lework/store/types/chat";
import type { PayrollSelectedFile } from "./payroll-accounting-mock";

export function revokePayrollPreviewUrl(url?: string) {
	if (url?.startsWith("blob:")) URL.revokeObjectURL(url);
}

export async function uploadPayrollAttachments(
	files: PayrollSelectedFile[],
	projectId: string,
): Promise<Attachment[]> {
	const attachments: Attachment[] = [];
	for (const selected of files) {
		if (selected.publicId) {
			attachments.push({
				id: selected.id,
				type: "file",
				name: selected.name,
				size: selected.size,
				fileUploadId: selected.publicId,
				mimeType: selected.mimeType,
				storageUri: selected.storageUri,
				uploadStatus: "completed",
				attachmentRole: selected.role,
			});
			continue;
		}
		if (!selected.file) throw new Error(`文件「${selected.name}」缺少内容`);
		const response = projectId
			? await projectFileApi.upload({
					projectId,
					projectPublicId: projectId,
					file: selected.file,
				})
			: await projectFileApi.uploadLoose({
					file: selected.file,
				});
		const uploaded = response.data;
		if (!uploaded?.public_id) throw new Error(`文件「${selected.name}」上传失败`);
		attachments.push({
			id: selected.id,
			type: "file",
			name: selected.name,
			size: selected.size,
			fileUploadId: uploaded.public_id,
			mimeType: selected.mimeType,
			storageUri: uploaded.storage_uri,
			uploadStatus: "completed",
			attachmentRole: selected.role,
		});
	}
	return attachments;
}

export function appendPayrollLocalFiles(
	current: PayrollSelectedFile[],
	selected: FileList | null,
	role: PayrollSelectedFile["role"],
): PayrollSelectedFile[] {
	const nextFiles = Array.from(selected ?? []);
	if (!nextFiles.length) return current;
	const existing = new Set(
		current
			.filter((item) => item.file)
			.map(({ file }) => `${file?.name}:${file?.size}:${file?.lastModified}`),
	);
	const additions = nextFiles
		.filter((file) => {
			const key = `${file.name}:${file.size}:${file.lastModified}`;
			if (existing.has(key)) return false;
			existing.add(key);
			return true;
		})
		.map((file) => ({
			id: `payroll-${crypto.randomUUID()}`,
			file,
			name: file.name,
			mimeType: file.type,
			size: file.size,
			role,
			previewUrl: URL.createObjectURL(file),
		}));
	return [...current, ...additions];
}
