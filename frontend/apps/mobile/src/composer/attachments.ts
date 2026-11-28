import type { ApiClient } from "@lework/api";

export interface PickedAttachment {
	uri: string;
	name: string;
	mimeType?: string;
	size?: number;
}

/** 打开系统文件选择器；取消时返回空数组，方便调用方直接展开合并。 */
export async function pickAttachments(): Promise<PickedAttachment[]> {
	const DocumentPicker = await import("expo-document-picker");
	const result = await DocumentPicker.getDocumentAsync({
		multiple: true,
		copyToCacheDirectory: true,
	});
	if (result.canceled) return [];
	return result.assets.map((asset) => ({
		uri: asset.uri,
		name: asset.name,
		mimeType: asset.mimeType,
		size: asset.size,
	}));
}

/** 逐个上传附件并返回最终落盘路径；任一失败即抛出，由调用方保留草稿。 */
export async function uploadAttachments(
	client: ApiClient,
	nodeID: string,
	directory: string,
	files: PickedAttachment[],
): Promise<string[]> {
	const paths: string[] = [];
	for (const file of files) {
		const form = new FormData();
		// React Native 的 FormData 直接接受 { uri, name, type } 描述本地文件。
		form.append("file", {
			uri: file.uri,
			name: file.name,
			type: file.mimeType ?? "application/octet-stream",
		} as unknown as Blob);
		const uploaded = await client.uploadFile(nodeID, directory, form);
		paths.push(uploaded.path);
	}
	return paths;
}
