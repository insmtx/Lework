import type { Message, PendingPermission, SessionState } from "@lework/api";
import { ApiError } from "@lework/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { createClient } from "../api/client";
import {
	applyLiveEvent,
	clearBuffer,
	createLiveBuffer,
	type LiveBuffer,
	mergeSnapshot,
} from "../events/live-buffer";
import { isResyncEvent } from "../events/merge";
import { needsRefetch } from "../events/structural";
import { useControlEvents } from "../events/use-control-events";
import type { Settings } from "../settings/settings";
import { messageText } from "./messages";
import { mergePermission } from "./permissions";

type Decision = "once" | "always" | "reject";

/** REFETCH_DEBOUNCE_MS 把结构性事件合并成一次快照请求。 */
const REFETCH_DEBOUNCE_MS = 500;

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

/** contentSignature 只关心「可见文本是否变长」，用来判断跟随期间有没有新内容。 */
function contentSignature(messages: Message[]): string {
	let total = 0;
	for (const message of messages) total += messageText(message).length;
	return `${messages.length}:${total}`;
}

// useSession 维护一个会话的只读视图与审批列表。
//
// 打开页面先向服务端要快照（消息 + 待审批），事件只做增量；回复永远以
// permission ID 提交，绝不把本地卡片当作审批状态。
//
// 流式文本单独存 live 缓冲：快照只允许延伸、不允许缩短（见 live-buffer），
// 所以 delta 与快照交错时不会出现重复或回缩的文本。执行状态由事件维护，
// 页面若在 overview 里命中则以 overview 为准（它是服务端投影）。
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
	/** state 是页面本地从事件派生的执行状态；overview 命中时由页面覆盖。 */
	state: SessionState | null;
	/** hasNewBelow 表示跟随已暂停（用户向上翻历史）期间又来了新内容。 */
	hasNewBelow: boolean;
	/** setFollowing 由页面在滚动时同步：贴底 true，翻历史 false。 */
	setFollowing: (following: boolean) => void;
	/** jumpToLatest 恢复跟随并清掉「有新内容」。 */
	jumpToLatest: () => void;
} {
	const client = useMemo(() => (settings ? createClient(settings) : null), [settings]);
	const [snapshot, setSnapshot] = useState<Message[]>([]);
	const [buffer, setBuffer] = useState<LiveBuffer>(createLiveBuffer);
	const [permissions, setPermissionsState] = useState<PendingPermission[]>([]);
	// 事件回调读不到最新的 React 状态，用 ref 保留一份同步镜像：
	// 判断「是否还有待审批」依赖它，旧值会让状态点在回复后卡住。
	const permissionsRef = useRef<PendingPermission[]>([]);
	const setPermissions = useCallback((next: PendingPermission[]) => {
		permissionsRef.current = next;
		setPermissionsState(next);
	}, []);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [sending, setSending] = useState(false);
	const [notFound, setNotFound] = useState(false);
	const [state, setState] = useState<SessionState | null>(null);
	const [following, setFollowingState] = useState(true);
	const [hasNewBelow, setHasNewBelow] = useState(false);

	// 快照与缓冲分开存，messages 是两者的投影；渲染期间不修改缓冲。
	const messages = useMemo(() => mergeSnapshot(snapshot, buffer).messages, [snapshot, buffer]);

	const loadMessages = useCallback(async () => {
		if (!client || !sessionID) return;
		// 只读移动端不需要回溯历史，取最新一页即可；后续要做“加载更早”再消费 next。
		const page = await client.listMessages(nodeID, sessionID);
		setSnapshot(page.messages);
		// 快照已经覆盖到的缓冲条目就地丢弃，避免同一段文本被两条路径重复维护。
		setBuffer((current) => mergeSnapshot(page.messages, current).buffer);
	}, [client, nodeID, sessionID]);

	const loadPermissions = useCallback(async () => {
		if (!client || !sessionID) return;
		setPermissions(await client.listPendingPermissions(nodeID, sessionID));
	}, [client, nodeID, sessionID, setPermissions]);

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
			// 节点重连后事件可能断档：清掉本地增量与派生状态，整页重拉一次。
			if (isResyncEvent(event, nodeID)) {
				setBuffer(clearBuffer());
				setState(null);
				refresh();
				return;
			}
			// session 过滤会保留节点级事件（如 resync），其余会话的事件仍在上面拦掉。
			if (event.node_id !== nodeID || event.session_id !== sessionID) return;

			const permissionEvent =
				event.type === "permission.asked" ||
				event.type === "permission.replied" ||
				event.type === "permission.resolved";
			if (permissionEvent) {
				const next = mergePermission(permissionsRef.current, event);
				setPermissions(next);
				// 审批状态跟随实际待审批数量：还有卡片就必须继续显示橙点。
				setState(next.length > 0 ? "waiting_permission" : "running");
			}

			switch (event.type) {
				case "session.execution.started":
					setState("running");
					break;
				case "session.execution.succeeded":
				case "session.execution.interrupted":
					// 执行结束意味着本轮不可能再有待审批，服务端也会清空。
					setPermissions([]);
					setState("idle");
					break;
				case "session.execution.failed":
					setPermissions([]);
					setState("error");
					break;
				default:
					break;
			}

			// 增量只进本地缓冲（只在 live=1 时才会到达），不触发整页重拉。
			if (event.type === "session.text.delta" || event.type === "session.text.ended") {
				setBuffer((previous) => applyLiveEvent(previous, event));
			}
			if (!needsRefetch(event.type)) return;

			// 消息流更新频繁，合并成一次拉取，避免每个结构事件都打服务端。
			if (debounce.current) clearTimeout(debounce.current);
			debounce.current = setTimeout(() => {
				void loadMessages().catch(ignoreRefreshError);
			}, REFETCH_DEBOUNCE_MS);
		},
		() => {
			// 断线重连：本地增量与派生状态都可能落后，清空后从快照重建。
			setBuffer(clearBuffer());
			setState(null);
			void loadMessages().catch(ignoreRefreshError);
			void loadPermissions().catch(ignoreRefreshError);
		},
		// live=1 才收增量事件；session 过滤把这条流收窄到当前会话（节点级事件保留）。
		{ nodeID, sessionID, live: true },
	);

	// 跟随暂停期间内容变长时亮「有新内容」；恢复跟随后签名重新对齐。
	const signature = useMemo(() => contentSignature(messages), [messages]);
	const followedSignature = useRef(signature);
	useEffect(() => {
		if (following) {
			followedSignature.current = signature;
			setHasNewBelow(false);
			return;
		}
		if (signature !== followedSignature.current) setHasNewBelow(true);
	}, [signature, following]);

	const setFollowing = useCallback((next: boolean) => {
		setFollowingState(next);
	}, []);

	const jumpToLatest = useCallback(() => {
		setFollowingState(true);
		setHasNewBelow(false);
	}, []);

	const reply = useCallback(
		async (permissionID: string, decision: Decision, message?: string) => {
			if (!client) throw new Error("not connected");
			await client.replyPermission(nodeID, sessionID, permissionID, decision, message);
			setPermissions(permissionsRef.current.filter((item) => item.id !== permissionID));
		},
		[client, nodeID, sessionID, setPermissions],
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
		state,
		hasNewBelow,
		setFollowing,
		jumpToLatest,
	};
}
