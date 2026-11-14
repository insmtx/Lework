/**
 * runSend 只在发送成功后清空草稿。
 *
 * 抽成纯函数是为了让失败路径可测：建会话成功但 prompt 失败时，界面必须
 * 保留用户输入，否则用户会以为消息已经发出。
 */
export async function runSend(send: () => Promise<void>, onClear: () => void): Promise<void> {
	await send();
	onClear();
}
