import type { NodeView } from "@lework/api";

/**
 * sortNodes 让在线的电脑排在前面，同状态内按名称、再按 ID 排序。
 *
 * 控制面返回的顺序是按 ID 排的，和「哪台现在能用」无关；节点一多，在线
 * 设备会散落在列表中间，用户得逐条去看状态。这里只做展示排序，不改数据。
 */
export function sortNodes(nodes: NodeView[]): NodeView[] {
	return [...nodes].sort((a, b) => {
		if (a.connected !== b.connected) return a.connected ? -1 : 1;
		// 不用 localeCompare：它依赖设备的语言环境，iOS/Android 可能给出不一致的顺序。
		if (a.name !== b.name) return a.name < b.name ? -1 : 1;
		return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
	});
}
