import type { Session } from "@lework/api";
import { useCallback, useEffect, useState } from "react";

import { createClient } from "../api/client";
import type { Settings } from "../settings/settings";
import { sortSessionsByActivity } from "./select";

export interface NodeSessionsState {
	sessions: Session[];
	loading: boolean;
	error: string | null;
	refresh: () => void;
}

/**
 * useNodeSessions 拉取一个节点上的全部会话。
 *
 * 会话页用它一次性拿到标题和同项目兄弟会话：列表接口已经包含标题、
 * projectID 与 updatedAt，不必为切换器再单独查会话详情。
 */
export function useNodeSessions(settings: Settings | null, nodeID: string): NodeSessionsState {
	const [sessions, setSessions] = useState<Session[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const refresh = useCallback(() => {
		if (!settings || nodeID === "") {
			setSessions([]);
			setLoading(false);
			return;
		}

		setLoading(true);
		setError(null);
		void createClient(settings)
			.listSessions(nodeID)
			.then((list) => {
				setSessions(
					sortSessionsByActivity(list.map((session) => ({ nodeID, session }))).map(
						(ref) => ref.session,
					),
				);
			})
			.catch((caught: unknown) => {
				setError(caught instanceof Error ? caught.message : "无法读取会话列表");
			})
			.finally(() => setLoading(false));
	}, [settings, nodeID]);

	useEffect(() => {
		refresh();
	}, [refresh]);

	return { sessions, loading, error, refresh };
}
