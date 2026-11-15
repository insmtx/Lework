import type { Session } from "@lework/api";

/** SubagentPickHints 描述这次工具调用的可见线索，用于缩小候选子会话。 */
export interface SubagentPickHints {
	/** 工具输入里的任务描述；OpenCode 创建子会话时通常把它当标题。 */
	description: string;
	/**
	 * 工具是否仍在执行。执行中的子会话必然处于 running，这是标题对不上时
	 * 最可靠的归属线索；完成后则不再猜，避免把历史会话认成这一次。
	 */
	running?: boolean;
}

// updatedMillis 把 ISO 时间转成毫秒；缺失或非法时返回 0，让它们排在最后。
function updatedMillis(session: Session): number {
	if (!session.updatedAt) return 0;
	const at = Date.parse(session.updatedAt);
	return Number.isNaN(at) ? 0 : at;
}

/**
 * pickSubagentSession 从父会话的子会话里挑出这次 subagent 调用对应的那个。
 *
 * 优先级从强到弱：
 * 1. 标题与 description 完全一致（OpenCode 默认用 description 当标题）；
 * 2. 工具仍在执行时，唯一的 running 子会话（同标题有多个时优先 running 的）；
 * 3. 只有一个子会话时直接用它。
 *
 * 都不满足时返回 null 退回静态内容，而不是拿「最近更新」硬猜：认错会话会让
 * 用户看到另一个代理的对话。
 */
export function pickSubagentSession(children: Session[], hints: SubagentPickHints): Session | null {
	const target = hints.description.trim();
	if (target !== "") {
		const matches = children.filter((session) => (session.title ?? "").trim() === target);
		if (matches.length > 0) {
			matches.sort((left, right) => {
				// 重新执行同一任务时会复用同标题的子会话，running 的是本次。
				if (hints.running) {
					const active = Number(right.running === true) - Number(left.running === true);
					if (active !== 0) return active;
				}
				return updatedMillis(right) - updatedMillis(left);
			});
			return matches[0] ?? null;
		}
	}

	if (hints.running) {
		const active = children
			.filter((session) => session.running === true)
			.sort((left, right) => updatedMillis(right) - updatedMillis(left));
		if (active.length > 0) return active[0] ?? null;
	}

	if (children.length === 1) return children[0] ?? null;
	return null;
}
