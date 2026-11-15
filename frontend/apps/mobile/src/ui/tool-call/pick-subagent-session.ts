import type { Session } from "@lework/api";

// updatedMillis 把 ISO 时间转成毫秒；缺失或非法时返回 0，让它们排在最后。
function updatedMillis(session: Session): number {
	if (!session.updatedAt) return 0;
	const at = Date.parse(session.updatedAt);
	return Number.isNaN(at) ? 0 : at;
}

/**
 * pickSubagentSession 从父会话的子会话里挑出这次 subagent 调用对应的那个。
 *
 * 执行中的工具记录里没有子会话 id，只有 description；OpenCode 创建子会话时
 * 默认用 description 当标题，所以标题全等是最可靠的线索。标题对不上时，
 * 唯一子会话可以确定归属；有多个就宁可返回 null 退回静态内容，也不猜——
 * 认错会话会让用户看到另一个代理的对话。
 */
export function pickSubagentSession(children: Session[], description: string): Session | null {
	const target = description.trim();
	if (target !== "") {
		const matches = children
			.filter((session) => (session.title ?? "").trim() === target)
			.sort((a, b) => updatedMillis(b) - updatedMillis(a));
		if (matches.length > 0) return matches[0] ?? null;
	}
	if (children.length === 1) return children[0] ?? null;
	return null;
}
