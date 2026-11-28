import { type ApiClient, attachmentNote, type Session } from "@lework/api";
import { type PickedAttachment, uploadAttachments } from "./attachments";

/** newIdempotencyKey 每次发送生成一个键，重试时由调用方复用同一个键。 */
export function newIdempotencyKey(nodeID: string, sessionID: string): string {
	return `${nodeID}:${sessionID}:${Date.now()}:${Math.random().toString(36).slice(2)}`;
}

/**
 * sendToProject 在目标节点上新建会话并立即发送首条消息。
 *
 * 附件先全部上传成功再建会话：中途失败时不会留下一个空会话，调用方也能
 * 保留草稿与已选附件。三步都成功才算发送完成。
 */
export async function sendToProject(
	client: ApiClient,
	nodeID: string,
	directory: string,
	text: string,
	idempotencyKey: string,
	files: PickedAttachment[] = [],
): Promise<Session> {
	const paths = await uploadAttachments(client, nodeID, directory, files);
	const session = await client.createSession(nodeID, directory);
	await client.sendPrompt(nodeID, session.id, text + attachmentNote(paths), idempotencyKey);
	return session;
}
