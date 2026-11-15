/**
 * needsRefetch 判断事件是否需要重拉消息快照。
 *
 * 只有 `*.delta` 是纯增量：它们进本地缓冲即可。其余事件——`session.tool.*`、
 * `session.execution.*`、`session.text.ended`、`session.created/updated`、
 * `permission.*`——都会改变消息列表结构或某条消息的权威内容，必须重拉；
 * 未知类型同样重拉，漏掉结构变化的代价比多拉一次大。
 *
 * 过去任意事件都触发整页重拉，一次长回答会重拉几十次、每次约 1MB。
 */
export function needsRefetch(eventType: string): boolean {
	return !eventType.endsWith(".delta");
}
