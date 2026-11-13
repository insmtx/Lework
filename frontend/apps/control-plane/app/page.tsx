"use client";

import { useIsMobile } from "@lework/ui/hooks/use-mobile";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { NodeSidebar } from "../components/node-sidebar";
import { SessionPanel } from "../components/session-panel";
import {
	createSession,
	deleteSession,
	getNodeHealth,
	interruptSession,
	listMessages,
	listNodes,
	listPermissions,
	listSessions,
	renameSession,
	replyPermission,
	restartOpenCode,
	sendPrompt,
	startOpenCode,
	stopOpenCode,
} from "../lib/api";
import type {
	ControlEvent,
	Message,
	NodeHealth,
	NodeView,
	PermissionRequest,
	Session,
} from "../lib/types";
import { useControlEvents } from "../lib/use-control-events";

type LifecycleAction = "start" | "restart" | "stop";

function errorMessage(error: unknown, fallback: string): string {
	return error instanceof Error ? error.message : fallback;
}

// mergeMessages keeps already-loaded older history while letting the newest
// page replace tool/output state for messages it contains.
function mergeMessages(base: Message[], incoming: Message[]): Message[] {
	const byID = new Map(incoming.map((message) => [message.id, message]));
	const merged = base.map((message) => byID.get(message.id) ?? message);
	const known = new Set(merged.map((message) => message.id));
	for (const message of incoming) {
		if (!known.has(message.id)) merged.push(message);
	}
	return merged;
}

function permissionFromEvent(event: ControlEvent): PermissionRequest | null {
	if (!event.data || typeof event.data !== "object") return null;
	const data = event.data as Partial<PermissionRequest>;
	if (!data.id || !data.action) return null;
	return {
		id: data.id,
		sessionID: data.sessionID || event.session_id || "",
		action: data.action,
		resources: Array.isArray(data.resources) ? data.resources : [],
		message: data.message ?? null,
	};
}

/** 从跨会话待审批表里移除一个权限；没有命中时返回原对象，避免多余渲染。 */
function removePending(
	current: Record<string, string[]>,
	permissionID: string,
): Record<string, string[]> {
	let changed = false;
	const next: Record<string, string[]> = {};
	for (const [sessionID, ids] of Object.entries(current)) {
		const filtered = ids.filter((id) => id !== permissionID);
		if (filtered.length !== ids.length) changed = true;
		if (filtered.length > 0) next[sessionID] = filtered;
	}
	return changed ? next : current;
}

export default function ControlPlanePage() {
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [nodesLoading, setNodesLoading] = useState(true);
	const [nodesError, setNodesError] = useState<string | null>(null);
	const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null);

	const [sessions, setSessions] = useState<Session[]>([]);
	const [sessionsLoading, setSessionsLoading] = useState(false);
	const [sessionsError, setSessionsError] = useState<string | null>(null);
	const [sessionsRefresh, setSessionsRefresh] = useState(0);
	const [sessionDirectory, setSessionDirectory] = useState("");
	const [selectedSessionID, setSelectedSessionID] = useState<string | null>(null);

	const [messages, setMessages] = useState<Message[]>([]);
	const [messagesLoading, setMessagesLoading] = useState(false);
	const [messagesError, setMessagesError] = useState<string | null>(null);
	const [messagesRefresh, setMessagesRefresh] = useState(0);
	const [nextCursor, setNextCursor] = useState<string | null>(null);
	const [loadingOlder, setLoadingOlder] = useState(false);
	const [permissions, setPermissions] = useState<PermissionRequest[]>([]);
	const [permissionsRefresh, setPermissionsRefresh] = useState(0);
	// 全局（跨会话）待审批项：手机上看不到别的会话，列表徽标是唯一入口。
	const [pendingBySession, setPendingBySession] = useState<Record<string, string[]>>({});
	const [sending, setSending] = useState(false);
	const [interrupting, setInterrupting] = useState(false);
	// 手机端单栏：先在节点/会话列表，选中会话后切到会话面板；桌面端忽略该状态。
	const [mobilePane, setMobilePane] = useState<"list" | "session">("list");
	const isMobile = useIsMobile();

	const [nodeHealth, setNodeHealth] = useState<NodeHealth | null>(null);
	const [nodeHealthError, setNodeHealthError] = useState<string | null>(null);
	const [lifecycleBusy, setLifecycleBusy] = useState<LifecycleAction | null>(null);

	const selectedNode = useMemo(
		() => nodes.find((node) => node.id === selectedNodeID) ?? null,
		[nodes, selectedNodeID],
	);
	const selectedSession = useMemo(
		() => sessions.find((session) => session.id === selectedSessionID) ?? null,
		[sessions, selectedSessionID],
	);

	const messagesTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
	const sessionsTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

	const refreshNodes = useCallback(async () => {
		try {
			const result = await listNodes();
			setNodes(result);
			setNodesError(null);
			setSelectedNodeID((current) => {
				if (current && result.some((node) => node.id === current)) return current;
				return (result.find((node) => node.connected) ?? result[0])?.id ?? null;
			});
		} catch (error) {
			setNodesError(errorMessage(error, "Unable to load nodes"));
		} finally {
			setNodesLoading(false);
		}
	}, []);

	useEffect(() => {
		void refreshNodes();
	}, [refreshNodes]);

	useEffect(() => {
		const timer = setInterval(() => void refreshNodes(), 15_000);
		return () => clearInterval(timer);
	}, [refreshNodes]);

	const refreshHealth = useCallback(async () => {
		if (!selectedNodeID) {
			setNodeHealth(null);
			setNodeHealthError(null);
			return;
		}
		try {
			const health = await getNodeHealth(selectedNodeID);
			setNodeHealth(health);
			setNodeHealthError(null);
		} catch (error) {
			setNodeHealth(null);
			setNodeHealthError(errorMessage(error, "Unable to load OpenCode status"));
		}
	}, [selectedNodeID]);

	useEffect(() => {
		void refreshHealth();
	}, [refreshHealth]);

	useEffect(() => {
		if (!selectedNodeID) return;
		const timer = setInterval(() => void refreshHealth(), 15_000);
		return () => clearInterval(timer);
	}, [selectedNodeID, refreshHealth]);

	// The token is an intentional trigger; the effect body reads the latest IDs.
	// biome-ignore lint/correctness/useExhaustiveDependencies: sessionsRefresh forces a refetch
	useEffect(() => {
		if (!selectedNodeID) {
			setSessions([]);
			setSelectedSessionID(null);
			return;
		}
		let cancelled = false;
		setSessionsLoading(true);
		setSessionsError(null);
		listSessions(selectedNodeID, sessionDirectory)
			.then((result) => {
				if (cancelled) return;
				setSessions(result);
				setSelectedSessionID((current) => {
					if (current && result.some((session) => session.id === current)) return current;
					return result[0]?.id ?? null;
				});
			})
			.catch((error: unknown) => {
				if (!cancelled) setSessionsError(errorMessage(error, "Unable to load sessions"));
			})
			.finally(() => {
				if (!cancelled) setSessionsLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [selectedNodeID, sessionDirectory, sessionsRefresh]);

	// 事件没有重放：选中会话先用快照补齐，再把事件新增的卡片并入。
	// biome-ignore lint/correctness/useExhaustiveDependencies: permissionsRefresh forces a refetch
	useEffect(() => {
		if (!selectedNodeID || !selectedSessionID) {
			setPermissions([]);
			return;
		}
		let cancelled = false;
		const sessionID = selectedSessionID;
		listPermissions(selectedNodeID, sessionID)
			.then((snapshot) => {
				if (cancelled) return;
				setPermissions((current) => {
					// 快照是权威列表；保留请求期间到达的事件卡片，避免漏掉新审批。
					const merged = new Map(current.map((item) => [item.id, item]));
					for (const item of snapshot) merged.set(item.id, item);
					const list = [...merged.values()];
					// 顺手把列表徽标对齐到服务端快照，避免断线期间漏掉或残留。
					setPendingBySession((previous) => {
						const ids = list.map((item) => item.id);
						if (ids.length === 0) {
							if (!(sessionID in previous)) return previous;
							const next = { ...previous };
							delete next[sessionID];
							return next;
						}
						return { ...previous, [sessionID]: ids };
					});
					return list;
				});
			})
			.catch(() => {
				// 旧版 OpenCode 没有该端点时降级为纯事件模式，不阻塞会话。
			});
		return () => {
			cancelled = true;
		};
	}, [selectedNodeID, selectedSessionID, permissionsRefresh]);

	// 切换会话时清空旧消息；分页游标不能跨会话复用。
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset history when the selection changes
	useEffect(() => {
		setMessages([]);
		setNextCursor(null);
		setLoadingOlder(false);
	}, [selectedNodeID, selectedSessionID]);

	// The token is an intentional trigger; the effect body reads the latest IDs.
	// biome-ignore lint/correctness/useExhaustiveDependencies: messagesRefresh forces a refetch
	useEffect(() => {
		if (!selectedNodeID || !selectedSessionID) {
			setMessages([]);
			setNextCursor(null);
			return;
		}
		let cancelled = false;
		setMessagesLoading(true);
		setMessagesError(null);
		listMessages(selectedNodeID, selectedSessionID)
			.then((page) => {
				if (cancelled) return;
				setMessages((current) => mergeMessages(current, page.messages));
				setNextCursor(page.next ?? null);
			})
			.catch((error: unknown) => {
				if (!cancelled) setMessagesError(errorMessage(error, "Unable to load messages"));
			})
			.finally(() => {
				if (!cancelled) setMessagesLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [selectedNodeID, selectedSessionID, messagesRefresh]);

	const loadOlder = async () => {
		if (!selectedNodeID || !selectedSessionID || !nextCursor || loadingOlder) return;
		setLoadingOlder(true);
		setMessagesError(null);
		try {
			const page = await listMessages(selectedNodeID, selectedSessionID, nextCursor);
			setMessages((current) => {
				const known = new Set(current.map((message) => message.id));
				const older = page.messages.filter((message) => !known.has(message.id));
				return [...older, ...current];
			});
			setNextCursor(page.next ?? null);
		} catch (error) {
			setMessagesError(errorMessage(error, "Unable to load earlier messages"));
		} finally {
			setLoadingOlder(false);
		}
	};

	useEffect(() => {
		return () => {
			if (messagesTimer.current) clearTimeout(messagesTimer.current);
			if (sessionsTimer.current) clearTimeout(sessionsTimer.current);
		};
	}, []);

	const handleEvent = (event: ControlEvent) => {
		if (event.type === "permission.asked") {
			const permission = permissionFromEvent(event);
			if (!permission) return;
			const sessionID = permission.sessionID || event.session_id || "";
			if (sessionID) {
				setPendingBySession((current) => {
					const ids = current[sessionID] ?? [];
					if (ids.includes(permission.id)) return current;
					return { ...current, [sessionID]: [...ids, permission.id] };
				});
			}
			if (sessionID && sessionID === selectedSessionID) {
				setPermissions((current) =>
					current.some((item) => item.id === permission.id) ? current : [...current, permission],
				);
			}
			return;
		}
		if (event.type === "permission.replied" || event.type === "permission.resolved") {
			// V2 的已决事件用 requestID 指向原请求，没有 id 字段。
			const data =
				event.data && typeof event.data === "object"
					? (event.data as { requestID?: string; id?: string })
					: undefined;
			const id = data?.requestID ?? data?.id;
			if (id) {
				setPendingBySession((current) => removePending(current, id));
				setPermissions((current) => current.filter((item) => item.id !== id));
			}
			return;
		}
		if (!selectedSessionID || event.session_id !== selectedSessionID) return;

		if (messagesTimer.current) clearTimeout(messagesTimer.current);
		messagesTimer.current = setTimeout(() => setMessagesRefresh((value) => value + 1), 250);

		if (event.type === "session.updated" || event.type === "session.created") {
			if (sessionsTimer.current) clearTimeout(sessionsTimer.current);
			sessionsTimer.current = setTimeout(() => setSessionsRefresh((value) => value + 1), 500);
		}
	};

	const resync = useCallback(() => {
		void refreshNodes();
		setSessionsRefresh((value) => value + 1);
		setMessagesRefresh((value) => value + 1);
		setPermissionsRefresh((value) => value + 1);
	}, [refreshNodes]);

	const { connected } = useControlEvents(handleEvent, resync);

	// 流刚恢复时事件可能已经断档，先对账一次再继续增量。
	const wasConnected = useRef(false);
	useEffect(() => {
		if (connected && !wasConnected.current) resync();
		wasConnected.current = connected;
	}, [connected, resync]);

	// SSE 不可用时（反代缓冲、断线重连）用轮询兜底，保证对话仍能推进。
	useEffect(() => {
		if (connected) return;
		const timer = setInterval(() => {
			if (selectedSessionID) setMessagesRefresh((value) => value + 1);
		}, 5_000);
		return () => clearInterval(timer);
	}, [connected, selectedSessionID]);

	useEffect(() => {
		if (connected) return;
		if (!selectedNodeID) return;
		const timer = setInterval(() => setSessionsRefresh((value) => value + 1), 15_000);
		return () => clearInterval(timer);
	}, [connected, selectedNodeID]);

	// 手机端会话页是 pushState 出来的虚拟层级，系统返回键应回到列表而不是退出。
	useEffect(() => {
		if (!isMobile) return;
		const onPopState = () => setMobilePane("list");
		window.addEventListener("popstate", onPopState);
		return () => window.removeEventListener("popstate", onPopState);
	}, [isMobile]);

	const openSessionPane = (sessionID: string) => {
		setSelectedSessionID(sessionID);
		setMobilePane("session");
		if (isMobile) window.history.pushState({ leworkPane: "session" }, "");
	};

	const closeSessionPane = () => {
		if (typeof window !== "undefined" && window.history.state?.leworkPane === "session") {
			window.history.back();
			return;
		}
		setMobilePane("list");
	};

	const selectNode = (nodeID: string) => {
		setSelectedNodeID(nodeID);
		setSelectedSessionID(null);
		setSessionDirectory("");
		setMessages([]);
		setPermissions([]);
		setMobilePane("list");
	};

	const selectSession = (sessionID: string) => {
		openSessionPane(sessionID);
	};

	const create = async (directory: string) => {
		if (!selectedNodeID) throw new Error("Select a connected node first");
		const session = await createSession(selectedNodeID, directory);
		// 清空过滤器，确保刚创建的会话出现在列表中。
		setSessionDirectory("");
		openSessionPane(session.id);
		setSessionsRefresh((value) => value + 1);
	};

	const rename = async (sessionID: string, title: string) => {
		if (!selectedNodeID) throw new Error("Select a connected node first");
		await renameSession(selectedNodeID, sessionID, title);
		setSessions((current) =>
			current.map((session) => (session.id === sessionID ? { ...session, title } : session)),
		);
		setSessionsRefresh((value) => value + 1);
	};

	const remove = async (sessionID: string) => {
		if (!selectedNodeID) throw new Error("Select a connected node first");
		await deleteSession(selectedNodeID, sessionID);
		setSessions((current) => current.filter((session) => session.id !== sessionID));
		if (selectedSessionID === sessionID) {
			setSelectedSessionID(null);
			setMessages([]);
			setPermissions([]);
			setMobilePane("list");
		}
		setSessionsRefresh((value) => value + 1);
	};

	const send = async (text: string) => {
		if (!selectedNodeID || !selectedSessionID) throw new Error("No session selected");
		setSending(true);
		try {
			await sendPrompt(
				selectedNodeID,
				selectedSessionID,
				text,
				typeof crypto !== "undefined" && crypto.randomUUID
					? crypto.randomUUID()
					: `${Date.now()}-${Math.random()}`,
			);
			setMessagesRefresh((value) => value + 1);
			setSessionsRefresh((value) => value + 1);
		} finally {
			setSending(false);
		}
	};

	const interrupt = async () => {
		if (!selectedNodeID || !selectedSessionID) return;
		setInterrupting(true);
		try {
			await interruptSession(selectedNodeID, selectedSessionID);
			setMessagesRefresh((value) => value + 1);
		} catch {
			// The button is best-effort; the SSE refresh will reconcile state.
		} finally {
			setInterrupting(false);
		}
	};

	const resolvePermission = async (
		permission: PermissionRequest,
		decision: "once" | "always" | "reject",
	) => {
		if (!selectedNodeID || !selectedSessionID) throw new Error("No session selected");
		await replyPermission(selectedNodeID, selectedSessionID, permission.id, decision);
		setPermissions((current) => current.filter((item) => item.id !== permission.id));
		setPendingBySession((current) => removePending(current, permission.id));
		setMessagesRefresh((value) => value + 1);
	};

	const runLifecycle = async (action: LifecycleAction) => {
		if (!selectedNodeID) return;
		setLifecycleBusy(action);
		setNodeHealthError(null);
		try {
			const call =
				action === "start" ? startOpenCode : action === "restart" ? restartOpenCode : stopOpenCode;
			await call(selectedNodeID);
		} catch (error) {
			setNodeHealthError(errorMessage(error, `Unable to ${action} OpenCode`));
		} finally {
			setLifecycleBusy(null);
			await refreshHealth();
			setSessionsRefresh((value) => value + 1);
		}
	};

	const refresh = () => {
		setSessionsRefresh((value) => value + 1);
		setMessagesRefresh((value) => value + 1);
		setPermissionsRefresh((value) => value + 1);
	};

	// 会话级待审批数量：列表徽标与手机端头部计数都从这里取。
	const pendingCounts = useMemo(() => {
		const counts: Record<string, number> = {};
		for (const [sessionID, ids] of Object.entries(pendingBySession)) {
			if (ids.length > 0) counts[sessionID] = ids.length;
		}
		return counts;
	}, [pendingBySession]);

	const topError = nodesError || sessionsError || messagesError;

	const sidebar = (
		<NodeSidebar
			nodes={nodes}
			selectedNodeID={selectedNodeID}
			onSelectNode={selectNode}
			sessions={sessions}
			selectedSessionID={selectedSessionID}
			onSelectSession={selectSession}
			onCreateSession={create}
			onRefresh={refresh}
			loadingSessions={sessionsLoading}
			sessionDirectory={sessionDirectory}
			onFilterSessions={setSessionDirectory}
			onRenameSession={rename}
			onDeleteSession={remove}
			nodeHealth={nodeHealth}
			nodeHealthError={nodeHealthError}
			lifecycleBusy={lifecycleBusy}
			onStartOpenCode={() => runLifecycle("start")}
			onRestartOpenCode={() => runLifecycle("restart")}
			onStopOpenCode={() => runLifecycle("stop")}
			pendingCounts={pendingCounts}
		/>
	);

	const panel = (
		<SessionPanel
			node={selectedNode}
			session={selectedSession}
			messages={messages}
			permissions={permissions}
			streamConnected={connected}
			loadingMessages={messagesLoading}
			sending={sending}
			interrupting={interrupting}
			onSend={send}
			onInterrupt={interrupt}
			onResolvePermission={resolvePermission}
			onRefresh={refresh}
			hasOlder={Boolean(nextCursor)}
			loadingOlder={loadingOlder}
			onLoadOlder={loadOlder}
			onBack={isMobile ? closeSessionPane : undefined}
		/>
	);

	// 手机端单栏：列表与会话二选一，避免侧栏把会话区挤出屏幕。
	if (isMobile) {
		return (
			<div className="flex h-dvh flex-col overflow-hidden">
				{topError ? (
					<div className="border-b border-destructive/20 bg-destructive/5 px-3 py-1.5 text-[11px] text-destructive md:px-4 md:py-2 md:text-xs">
						{topError}
					</div>
				) : null}
				{mobilePane === "session" && selectedSession ? panel : sidebar}
			</div>
		);
	}

	return (
		<div className="flex h-dvh overflow-hidden">
			{sidebar}
			<main className="flex min-w-0 flex-1 flex-col">
				{nodesLoading ? (
					<div className="flex h-14 items-center border-b px-4 text-sm text-muted-foreground">
						Loading control plane…
					</div>
				) : null}
				{topError ? (
					<div className="border-b border-destructive/20 bg-destructive/5 px-3 py-1.5 text-[11px] text-destructive md:px-4 md:py-2 md:text-xs">
						{topError}
					</div>
				) : null}
				{panel}
			</main>
		</div>
	);
}
