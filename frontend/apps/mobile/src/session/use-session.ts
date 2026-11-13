import type { Message, PendingPermission } from "@lework/api";
import { ApiError } from "@lework/api";
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

/** isSessionMissing 识别「会话不存在」，兼容 404 与包装后的 agent_error。 */
function isSessionMissing(caught: unknown): boolean {
	if (!(caught instanceof ApiError)) return false;
	if (caught.status === 404) return true;
	return caught.status === 502 && /not\s*found|notfound/i.test(caught.message);
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
	/** sending 表示 prompt 正在提交，避免重复点击。 */
	sending: boolean;
	/** notFound 表示会话已不存在（深链过期或消息被删）。 */
	notFound: boolean;
} {
	const client = useMemo(() => (settings ? createClient(settings) : null), [settings]);
	const [messages, setMessages] = useState<Message[]>([]);
	const [permissions, setPermissions] = useState<PendingPermission[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [sending, setSending] = useState(false);
	const [notFound, setNotFound] = useState(false);

	const loadMessages = useCallback(async () => {
		if (!client) return;
		// 只读移动端不需要回溯历史，取最新一页即可；后续要做“加载更早”再消费 next。
		const page = await client.listMessages(nodeID, sessionID);
		setMessages(page.messages);
	}, [client, nodeID, sessionID]);

	const loadPermissions = useCallback(async () => {
		if (!client) return;
		setPermissions(await client.listPendingPermissions(nodeID, sessionID));
	}, [client, nodeID, sessionID]);

	const refresh = useCallback(() => {
		if (!client) return;
		setLoading(true);
		setError(null);
		setNotFound(false);
		void Promise.all([loadMessages(), loadPermissions()])
			.catch((caught: unknown) => {
				// 会话被删除或深链过期时，整页没有可展示内容；单独标记出来，
				// 让页面给出「去待审批/回我的」出口而不是空消息流。
				// 控制面把 OpenCode 的 404 包成 502 agent_error，消息里带
				// "not found"，所以两个形状都要认。
				if (isSessionMissing(caught)) {
					setNotFound(true);
					return;
				}
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
			setSending(true);
			try {
				await client.sendPrompt(nodeID, sessionID, trimmed, idempotencyKey(nodeID, sessionID));
				await loadMessages();
			} finally {
				setSending(false);
			}
		},
		[client, nodeID, sessionID, loadMessages],
	);

	const interrupt = useCallback(async () => {
		if (!client) throw new Error("not connected");
		await client.interruptSession(nodeID, sessionID);
		await loadMessages();
	}, [client, nodeID, sessionID, loadMessages]);

	return {
		messages,
		permissions,
		loading,
		error,
		refresh,
		reply,
		send,
		interrupt,
		connected,
		sending,
		notFound,
	};
}
