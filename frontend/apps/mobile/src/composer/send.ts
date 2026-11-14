import type { ApiClient, Session } from "@lework/api";

/** newIdempotencyKey 每次发送生成一个键，重试时由调用方复用同一个键。 */
export function newIdempotencyKey(nodeID: string, sessionID: string): string {
	return `${nodeID}:${sessionID}:${Date.now()}:${Math.random().toString(36).slice(2)}`;
}

/**
 * sendToProject 在目标节点上新建会话并立即发送首条消息。
 *
 * 两步都成功才算发送完成：建会话成功但 prompt 失败时抛错，让调用方保留
 * 草稿。否则用户会以为消息已经发出，而服务端只有一个空会话。
 */
export async function sendToProject(
	client: ApiClient,
	nodeID: string,
	directory: string,
	text: string,
	idempotencyKey: string,
): Promise<Session> {
	const session = await client.createSession(nodeID, directory);
	await client.sendPrompt(nodeID, session.id, text, idempotencyKey);
	return session;
}
