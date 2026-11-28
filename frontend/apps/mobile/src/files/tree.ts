import type { FileInfo, ProjectSummary } from "@lework/api";

import { resolveTarget } from "../projects/model";

/** pickBrowseNode 保留用户手选的在线节点，否则回落到新建会话的默认规则。 */
export function pickBrowseNode(
	project: ProjectSummary,
	preferred: string | null,
	onlineNodeIDs: string[],
): string | null {
	if (preferred && project.nodes.some((node) => node.node_id === preferred && node.online)) {
		return preferred;
	}
	return resolveTarget(project, onlineNodeIDs).nodeID;
}

/** sortEntries 让目录排在文件前，同级按路径稳定排序。 */
export function sortEntries(entries: FileInfo[]): FileInfo[] {
	return [...entries].sort((left, right) => {
		if (left.type !== right.type) return left.type === "directory" ? -1 : 1;
		return left.path.localeCompare(right.path);
	});
}
