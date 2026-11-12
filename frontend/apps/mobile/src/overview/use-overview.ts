import type { OverviewNode } from "@lework/api";
import { useCallback, useEffect, useState } from "react";

import { createClient } from "../api/client";
import { applyEvent, createOverviewState, type OverviewState } from "../events/merge";
import { useControlEvents } from "../events/use-control-events";
import type { Settings } from "../settings/settings";

// useOverview 维护“快照 + 增量”的总览视图。
//
// 拉快照失败时保留旧数据并暴露错误：移动网络会抖，清空列表反而更差。
export function useOverview(settings: Settings | null): {
	nodes: OverviewNode[];
	loading: boolean;
	error: string | null;
	refresh: () => void;
	connected: boolean;
} {
	const [state, setState] = useState<OverviewState>(() => createOverviewState());
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const refresh = useCallback(() => {
		if (!settings) return;
		setLoading(true);
		setError(null);
		void createClient(settings)
			.getOverview()
			.then((snapshot) => {
				setState(createOverviewState(snapshot));
			})
			.catch((caught: unknown) => {
				setError(caught instanceof Error ? caught.message : "failed to load overview");
			})
			.finally(() => {
				setLoading(false);
			});
	}, [settings]);

	useEffect(() => {
		if (settings) refresh();
	}, [settings, refresh]);

	const { connected } = useControlEvents(
		settings,
		(event) => {
			setState((previous) => applyEvent(previous, event));
		},
		refresh,
	);

	return { nodes: state.nodes, loading, error, refresh, connected };
}
