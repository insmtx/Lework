import type { Session } from "@lework/api";
import { useCallback, useEffect, useState } from "react";

import { createClient } from "../api/client";
import type { ProjectSessionRef } from "../sessions/select";
import type { Settings } from "../settings/settings";

export interface SidebarSessionsState {
	sessions: ProjectSessionRef[];
	loading: boolean;
	refresh: () => void;
}

/**
 * useSidebarSessions 并发拉取所有在线节点的会话，供侧边栏按项目分组。
 *
 * 离线节点直接跳过：控制面没有会话快照，列出来也只会点进去失败。单个
 * 节点失败同样忽略，侧边栏是导航而不是错误面板；全部失败时也只是列表
 * 暂时为空，下拉刷新即可重试。
 */
export function useSidebarSessions(
	settings: Settings | null,
	onlineNodeIDs: string[],
): SidebarSessionsState {
	const [sessions, setSessions] = useState<ProjectSessionRef[]>([]);
	const [loading, setLoading] = useState(false);

	const nodeKey = onlineNodeIDs.join(",");

	const refresh = useCallback(() => {
		if (!settings) {
			setSessions([]);
			setLoading(false);
			return;
		}
		const targets = nodeKey.split(",").filter(Boolean);
		if (targets.length === 0) {
			setSessions([]);
			setLoading(false);
			return;
		}

		setLoading(true);
		const client = createClient(settings);
		void Promise.all(
			targets.map(async (nodeID): Promise<ProjectSessionRef[]> => {
				try {
					const list: Session[] = await client.listSessions(nodeID);
					return list.map((session) => ({ nodeID, session }));
				} catch {
					return [];
				}
			}),
		)
			.then((groups) => setSessions(groups.flat()))
			.finally(() => setLoading(false));
	}, [settings, nodeKey]);

	useEffect(() => {
		refresh();
	}, [refresh]);

	return { sessions, loading, refresh };
}
