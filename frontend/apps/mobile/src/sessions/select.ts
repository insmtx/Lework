import type { Session } from "@lework/api";

import { isPathPrefix } from "../projects/model";

/** SessionRef 把一个会话与其所在节点绑定，避免跨节点 id 冲突。 */
export interface ProjectSessionRef {
	nodeID: string;
	session: Session;
}

/**
 * sessionMatchesProject 判断会话是否属于给定项目。
 *
 * 优先用 OpenCode 的 projectID：会话可以在项目子目录里创建，按目录前缀
 * 过滤会漏。老控制面不返回 projectID 时才退回 canonical 前缀匹配。
 * 「global」聚合键额外要求目录落在该项目 canonical 之下，因为多个无提交
 * 仓库共享同一个 projectID。
 */
export function sessionMatchesProject(
	session: Session,
	projectKey: string,
	canonical: string,
): boolean {
	if (session.projectID) {
		if (projectKey.startsWith("global:")) {
			return session.projectID === "global" && isPathPrefix(canonical, session.directory ?? "");
		}
		return session.projectID === projectKey;
	}
	return isPathPrefix(canonical, session.directory ?? "");
}

/** selectProjectSessions 过滤出属于该项目的会话，保持服务端顺序。 */
export function selectProjectSessions(
	refs: ProjectSessionRef[],
	projectKey: string,
	canonical: string,
): ProjectSessionRef[] {
	return refs.filter((ref) => sessionMatchesProject(ref.session, projectKey, canonical));
}

/**
 * sortSessionsByActivity 按最近更新时间倒序；缺失时间的会话保持在末尾，
 * 不参与比较，避免把服务端顺序打乱。
 */
export function sortSessionsByActivity(refs: ProjectSessionRef[]): ProjectSessionRef[] {
	const stamp = (ref: ProjectSessionRef) => Date.parse(ref.session.updatedAt ?? "");
	return [...refs].sort((left, right) => {
		const leftAt = stamp(left);
		const rightAt = stamp(right);
		const leftValid = !Number.isNaN(leftAt);
		const rightValid = !Number.isNaN(rightAt);
		if (leftValid && rightValid && leftAt !== rightAt) return rightAt - leftAt;
		if (leftValid !== rightValid) return leftValid ? -1 : 1;
		return 0;
	});
}
