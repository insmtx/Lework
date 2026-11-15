import { ApiError, type NodeView } from "@lework/api";

/** visibleNodes 隐藏未归属的历史节点：手机不支持认领它们。 */
export function visibleNodes(nodes: NodeView[]): NodeView[] {
	return nodes.filter((node) => node.claimed !== false);
}

/** withoutNode 返回移除后的列表，用于确认接口返回后立即更新界面。 */
export function withoutNode(nodes: NodeView[], nodeID: string): NodeView[] {
	return nodes.filter((node) => node.id !== nodeID);
}

/** describeRemoveNode 是确认弹窗的正文：必须写清机器侧要做什么。 */
export function describeRemoveNode(name: string): string {
	return `移除后 ${name} 会立即断开；要重新关联，需要在那台机器上执行 lework agent --reset；这台电脑的项目也会从列表里消失。`;
}

/**
 * removeErrorMessage 把移除失败翻译成提示；返回 null 表示「就当成功」。
 *
 * 404 单独处理：节点可能已被另一台设备或另一个控制面实例删掉，此时用户要的
 * 结果已经达成，弹「节点不存在」只会让他以为操作失败而反复重试。
 */
export function removeErrorMessage(error: unknown): string | null {
	if (error instanceof ApiError) {
		if (error.status === 404) return null;
		if (error.status === 401) return "登录已过期，请重新登录";
		if (error.status === 403) return "只有节点的所有者可以移除它";
		return "移除失败，请稍后重试";
	}
	return "网络不通，请检查服务器地址";
}
