import type { Message, PendingPermission } from "@lework/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { createClient } from "../api/client";
import { isResyncEvent } from "../events/merge";
import { useControlEvents } from "../events/use-control-events";
import type { Settings } from "../settings/settings";
import { mergePermission } from "./permissions";

type Decision = "once" | "always" | "reject";

function idempotencyKey(nodeID: string, sessionID: string): string {
	return `${nodeID}:${sessionID}:${Date.now()}:${Math.random().toString(36).slice(2)}`;
}

// ignoreRefreshError 用于后台增量与对账：失败时保留旧数据，
// 下一次事件、重连或手动刷新会纠正，不必打扰用户。
function ignoreRefreshError(): void {
	// 故意留空。
}

// useSession 维护一个会话的只读视图与审批列表。
//
// 打开页面先向服务端要快照（消息 + 待审批），事件只做增量；回复永远以
// permission ID 提交，绝不把本地卡片当作审批状态。
export function useSession(
	settings: Settings | null,
	nodeID: string,
	sessionID: string,
): {
	messages: Message[];
	permissions: PendingPermission[];
	loading: boolean;
	error: string | null;
	refresh: () => void;
	reply: (permissionID: string, decision: Decision, message?: string) => Promise<void>;
	send: (text: string) => Promise<void>;
	interrupt: () => Promise<void>;
	connected: boolean;
} {
	const client = useMemo(() => (settings ? createClient(settings) : null), [settings]);
	const [messages, setMessages] = useState<Message[]>([]);
	const [permissions, setPermissions] = useState<PendingPermission[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const loadMessages = useCallback(async () => {
		if (!client) return;
		setMessages(await client.listMessages(nodeID, sessionID));
	}, [client, nodeID, sessionID]);

	const loadPermissions = useCallback(async () => {
		if (!client) return;
		setPermissions(await client.listPendingPermissions(nodeID, sessionID));
	}, [client, nodeID, sessionID]);

	const refresh = useCallback(() => {
		if (!client) return;
		setLoading(true);
		setError(null);
		void Promise.all([loadMessages(), loadPermissions()])
			.catch((caught: unknown) => {
				setError(caught instanceof Error ? caught.message : "failed to load session");
			})
			.finally(() => setLoading(false));
	}, [client, loadMessages, loadPermissions]);

	useEffect(() => {
		refresh();
	}, [refresh]);

	const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(() => {
		return () => {
			if (debounce.current) clearTimeout(debounce.current);
		};
	}, []);

	const { connected } = useControlEvents(
		settings,
		(event) => {
			// 节点重连后事件可能断档：整页重拉一次，别在旧消息上继续叠加增量。
			if (isResyncEvent(event, nodeID)) {
				refresh();
				return;
			}
			if (event.node_id !== nodeID || event.session_id !== sessionID) return;
			if (
				event.type === "permission.asked" ||
				event.type === "permission.replied" ||
				event.type === "permission.resolved"
			) {
				setPermissions((previous) => mergePermission(previous, event));
			}
			// 消息流更新频繁，合并成一次拉取，避免每个增量都打服务端。
			if (debounce.current) clearTimeout(debounce.current);
			debounce.current = setTimeout(() => {
				void loadMessages().catch(ignoreRefreshError);
			}, 500);
		},
		() => {
			void loadMessages().catch(ignoreRefreshError);
			void loadPermissions().catch(ignoreRefreshError);
		},
	);

	const reply = useCallback(
		async (permissionID: string, decision: Decision, message?: string) => {
			if (!client) throw new Error("not connected");
			await client.replyPermission(nodeID, sessionID, permissionID, decision, message);
			setPermissions((previous) => previous.filter((item) => item.id !== permissionID));
		},
		[client, nodeID, sessionID],
	);

	const send = useCallback(
		async (text: string) => {
			if (!client) throw new Error("not connected");
			const trimmed = text.trim();
			if (!trimmed) return;
			await client.sendPrompt(nodeID, sessionID, trimmed, idempotencyKey(nodeID, sessionID));
			await loadMessages();
		},
		[client, nodeID, sessionID, loadMessages],
	);

	const interrupt = useCallback(async () => {
		if (!client) throw new Error("not connected");
		await client.interruptSession(nodeID, sessionID);
		await loadMessages();
	}, [client, nodeID, sessionID, loadMessages]);

	return { messages, permissions, loading, error, refresh, reply, send, interrupt, connected };
}
