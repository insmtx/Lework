import { type ApiClient, attachmentNote } from "@lework/api";

/**
 * 先逐个上传附件，全部成功后再发 prompt；任一步失败都抛出，草稿由调用方保留。
 */
export async function sendWithAttachments(
	client: ApiClient,
	nodeID: string,
	sessionID: string,
	directory: string,
	text: string,
	idempotencyKey: string,
	files: File[],
): Promise<void> {
	const paths: string[] = [];
	for (const file of files) {
		const form = new FormData();
		form.append("file", file);
		const uploaded = await client.uploadFile(nodeID, directory, form);
		paths.push(uploaded.path);
	}
	await client.sendPrompt(nodeID, sessionID, text + attachmentNote(paths), idempotencyKey);
}
