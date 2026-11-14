import type { NodeView, ProjectSummary } from "@lework/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { createClient } from "../api/client";
import { useControlEvents } from "../events/use-control-events";
import type { Settings } from "../settings/settings";
import { type InboxResolution, resolveInbox } from "./model";

/** INBOX_DIR_PREFIX 是「我的目录」偏好的键前缀，后缀是节点 ID。 */
export const INBOX_DIR_PREFIX = "inbox.dir.";

export interface ProjectsState {
	projects: ProjectSummary[];
	nodes: NodeView[];
	/** 每个节点的「我的目录」：用户偏好优先，缺省取 workspace root。 */
	dirByNode: Record<string, string>;
	inbox: InboxResolution;
	loading: boolean;
	error: string | null;
	refresh: () => void;
	connected: boolean;
	setPrimaryNode: (projectKey: string, nodeID: string) => Promise<void>;
	setInboxDir: (nodeID: string, dir: string) => Promise<void>;
}

/**
 * useProjects 维护项目聚合快照（项目 + 节点 + 偏好）。
 *
 * 事件只触发重新拉取，不在客户端重放项目变更：控制面是唯一聚合者，
 * 客户端拿到的永远是完整视图。刷新做了 300ms 防抖，避免一批事件打出
 * 十几次请求。
 */
export function useProjects(settings: Settings | null): ProjectsState {
	const [projects, setProjects] = useState<ProjectSummary[]>([]);
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [preferences, setPreferences] = useState<Record<string, string>>({});
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

	const refresh = useCallback(() => {
		if (!settings) return;
		setLoading(true);
		setError(null);
		const client = createClient(settings);
		void Promise.all([client.listProjects(), client.listNodes(), client.getPreferences()])
			.then(([nextProjects, nextNodes, nextPreferences]) => {
				setProjects(nextProjects);
				setNodes(nextNodes);
				setPreferences(nextPreferences);
			})
			.catch((caught: unknown) => {
				setError(caught instanceof Error ? caught.message : "failed to load projects");
			})
			.finally(() => setLoading(false));
	}, [settings]);

	const scheduleRefresh = useCallback(() => {
		if (timerRef.current) clearTimeout(timerRef.current);
		timerRef.current = setTimeout(() => {
			timerRef.current = null;
			refresh();
		}, 300);
	}, [refresh]);

	useEffect(() => {
		if (settings) refresh();
	}, [settings, refresh]);

	useEffect(
		() => () => {
			if (timerRef.current) clearTimeout(timerRef.current);
		},
		[],
	);

	const { connected } = useControlEvents(settings, scheduleRefresh, refresh);

	const dirByNode = useMemo(() => {
		const out: Record<string, string> = {};
		for (const node of nodes) {
			const preferred = preferences[`${INBOX_DIR_PREFIX}${node.id}`];
			const dir = preferred || node.workspace_roots?.[0] || "";
			if (dir) out[node.id] = dir;
		}
		return out;
	}, [nodes, preferences]);

	const onlineNodeIDs = useMemo(
		() => nodes.filter((node) => node.connected).map((node) => node.id),
		[nodes],
	);

	const inbox = useMemo(
		() => resolveInbox(projects, dirByNode, onlineNodeIDs),
		[projects, dirByNode, onlineNodeIDs],
	);

	const setPrimaryNode = useCallback(
		async (projectKey: string, nodeID: string) => {
			if (!settings) return;
			await createClient(settings).setProjectPrimaryNode(projectKey, nodeID);
			refresh();
		},
		[settings, refresh],
	);

	const setInboxDir = useCallback(
		async (nodeID: string, dir: string) => {
			if (!settings) return;
			await createClient(settings).setPreference(`${INBOX_DIR_PREFIX}${nodeID}`, dir);
			refresh();
		},
		[settings, refresh],
	);

	return {
		projects,
		nodes,
		dirByNode,
		inbox,
		loading,
		error,
		refresh,
		connected,
		setPrimaryNode,
		setInboxDir,
	};
}
