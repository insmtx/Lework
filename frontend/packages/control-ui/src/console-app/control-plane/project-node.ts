import type { ProjectNode, ProjectSummary } from "../../types";

export interface ResolvedProjectNode {
	node: ProjectNode;
	canonical: string;
	online: boolean;
}

/** projectNodeCanonical 返回选中节点自己的检出路径，节点不存在时返回 null。 */
export function projectNodeCanonical(project: ProjectSummary, nodeID: string): string | null {
	const node = project.nodes.find((item) => item.node_id === nodeID);
	if (!node) return null;
	return node.canonical?.trim() || project.canonical;
}

/**
 * resolveProjectNode 选择工作区的默认节点。
 *
 * 主节点在线时优先用它；否则回落到任意在线节点，保证主节点离线时仍能工作。
 * 全部离线时保留主节点用于展示离线态，避免页面没有可解释的节点。
 */
export function resolveProjectNode(project: ProjectSummary): ResolvedProjectNode | null {
	const primary = project.primary_node_id
		? (project.nodes.find((item) => item.node_id === project.primary_node_id) ?? null)
		: null;
	const online = project.nodes.find((item) => item.online) ?? null;
	const node = (primary?.online ? primary : null) ?? online ?? primary ?? project.nodes[0];
	if (!node) return null;
	return { node, canonical: node.canonical?.trim() || project.canonical, online: node.online };
}
