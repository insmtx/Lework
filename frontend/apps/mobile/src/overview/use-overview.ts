import type { ControlEvent, OverviewNode } from "@lework/api";
import { useCallback, useEffect, useRef, useState } from "react";

import { createClient } from "../api/client";
import {
	applyEvent,
	createOverviewState,
	isResyncEvent,
	type OverviewState,
} from "../events/merge";
import { useControlEvents } from "../events/use-control-events";
import type { Settings } from "../settings/settings";

// useOverview 维护“快照 + 增量”的总览视图。
//
// 拉快照失败时保留旧数据并暴露错误：移动网络会抖，清空列表反而更差。
// onStateEvent 复用同一条 SSE 把原始状态事件交给调用方（例如本地完成提醒），
// 避免再开一条流。
//
// 这条流 keepAlive：App 切后台后只要进程还在，完成事件仍能到达并触发本地
// 提醒；进程被系统冻结/回收时连接自然失效，回前台会重连并重拉快照。
export function useOverview(
	settings: Settings | null,
	onStateEvent?: (event: ControlEvent) => void,
): {
	nodes: OverviewNode[];
	loading: boolean;
	error: string | null;
	refresh: () => void;
	connected: boolean;
} {
	const [state, setState] = useState<OverviewState>(() => createOverviewState());
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const onStateEventRef = useRef(onStateEvent);
	onStateEventRef.current = onStateEvent;

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
			onStateEventRef.current?.(event);
			// resync 会先清掉该节点的派生状态，接着用快照补回来；
			// 只清不拉会让总览一直空着。
			if (isResyncEvent(event)) refresh();
			setState((previous) => applyEvent(previous, event));
		},
		refresh,
		{ keepAlive: true },
	);

	return { nodes: state.nodes, loading, error, refresh, connected };
}
