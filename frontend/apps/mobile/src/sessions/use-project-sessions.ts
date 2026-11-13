import type { ProjectSummary, Session } from "@lework/api";
import { useCallback, useEffect, useState } from "react";

import { createClient } from "../api/client";
import type { Settings } from "../settings/settings";
import {
	type ProjectSessionRef,
	selectProjectSessions,
	sortSessionsByActivity,
} from "./select";

export interface ProjectSessionsState {
	rows: ProjectSessionRef[];
	loading: boolean;
	error: string | null;
	refresh: () => void;
}

/**
 * useProjectSessions 并发拉取项目所在节点的会话并过滤出该项目。
 *
 * 单个节点失败只影响它自己；全部失败才报错，避免一个离线节点让整个
 * 项目页变成错误页。
 */
export function useProjectSessions(
	settings: Settings | null,
	project: ProjectSummary | null,
	nodeIDs: string[],
): ProjectSessionsState {
	const [rows, setRows] = useState<ProjectSessionRef[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const nodeKey = nodeIDs.join(",");
	const projectKey = project?.key ?? "";
	const canonical = project?.canonical ?? "";

	const refresh = useCallback(() => {
		if (!settings || !project) {
			setRows([]);
			return;
		}
		const targets = nodeKey ? nodeKey.split(",") : [];
		if (targets.length === 0) {
			setRows([]);
			return;
		}

		setLoading(true);
		setError(null);
		const client = createClient(settings);
		let okCount = 0;
		void Promise.all(
			targets.map(async (nodeID): Promise<ProjectSessionRef[]> => {
				try {
					const sessions: Session[] = await client.listSessions(nodeID);
					okCount += 1;
					return sessions.map((session) => ({ nodeID, session }));
				} catch {
					return [];
				}
			}),
		)
			.then((groups) => {
				if (okCount === 0) setError("无法读取节点会话");
				const all = groups.flat();
				setRows(sortSessionsByActivity(selectProjectSessions(all, projectKey, canonical)));
			})
			.finally(() => setLoading(false));
	}, [settings, project, nodeKey, projectKey, canonical]);

	useEffect(() => {
		refresh();
	}, [refresh]);

	return { rows, loading, error, refresh };
}
