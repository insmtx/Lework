"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { NodeSidebar } from "../components/node-sidebar";
import { SessionPanel } from "../components/session-panel";
import {
	createSession,
	interruptSession,
	listMessages,
	listNodes,
	listPermissions,
	listSessions,
	replyPermission,
	sendPrompt,
} from "../lib/api";
import type { ControlEvent, Message, NodeView, PermissionRequest, Session } from "../lib/types";
import { useControlEvents } from "../lib/use-control-events";

function errorMessage(error: unknown, fallback: string): string {
	return error instanceof Error ? error.message : fallback;
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

export default function ControlPlanePage() {
	const [nodes, setNodes] = useState<NodeView[]>([]);
	const [nodesLoading, setNodesLoading] = useState(true);
	const [nodesError, setNodesError] = useState<string | null>(null);
	const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null);

	const [sessions, setSessions] = useState<Session[]>([]);
	const [sessionsLoading, setSessionsLoading] = useState(false);
	const [sessionsError, setSessionsError] = useState<string | null>(null);
	const [sessionsRefresh, setSessionsRefresh] = useState(0);
	const [selectedSessionID, setSelectedSessionID] = useState<string | null>(null);

	const [messages, setMessages] = useState<Message[]>([]);
	const [messagesLoading, setMessagesLoading] = useState(false);
	const [messagesError, setMessagesError] = useState<string | null>(null);
	const [messagesRefresh, setMessagesRefresh] = useState(0);
	const [permissions, setPermissions] = useState<PermissionRequest[]>([]);
	const [permissionsRefresh, setPermissionsRefresh] = useState(0);
	const [sending, setSending] = useState(false);
	const [interrupting, setInterrupting] = useState(false);

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
		listSessions(selectedNodeID)
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
	}, [selectedNodeID, sessionsRefresh]);

	// 事件没有重放：选中会话先用快照补齐，再把事件新增的卡片并入。
	// biome-ignore lint/correctness/useExhaustiveDependencies: permissionsRefresh forces a refetch
	useEffect(() => {
		if (!selectedNodeID || !selectedSessionID) {
			setPermissions([]);
			return;
		}
		let cancelled = false;
		listPermissions(selectedNodeID, selectedSessionID)
			.then((snapshot) => {
				if (cancelled) return;
				setPermissions((current) => {
					// 快照是权威列表；保留请求期间到达的事件卡片，避免漏掉新审批。
					const merged = new Map(current.map((item) => [item.id, item]));
					for (const item of snapshot) merged.set(item.id, item);
					return [...merged.values()];
				});
			})
			.catch(() => {
				// 旧版 OpenCode 没有该端点时降级为纯事件模式，不阻塞会话。
			});
		return () => {
			cancelled = true;
		};
	}, [selectedNodeID, selectedSessionID, permissionsRefresh]);

	// The token is an intentional trigger; the effect body reads the latest IDs.
	// biome-ignore lint/correctness/useExhaustiveDependencies: messagesRefresh forces a refetch
	useEffect(() => {
		if (!selectedNodeID || !selectedSessionID) {
			setMessages([]);
			return;
		}
		let cancelled = false;
		setMessagesLoading(true);
		setMessagesError(null);
		listMessages(selectedNodeID, selectedSessionID)
			.then((result) => {
				if (!cancelled) setMessages(result);
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

	useEffect(() => {
		return () => {
			if (messagesTimer.current) clearTimeout(messagesTimer.current);
			if (sessionsTimer.current) clearTimeout(sessionsTimer.current);
		};
	}, []);

	const handleEvent = (event: ControlEvent) => {
		if (event.session_id && event.session_id !== selectedSessionID) return;
		if (event.type === "permission.asked") {
			const permission = permissionFromEvent(event);
			if (permission) {
				setPermissions((current) =>
					current.some((item) => item.id === permission.id) ? current : [...current, permission],
				);
			}
			return;
		}
		if (event.type === "permission.replied") {
			// V2 的已决事件用 requestID 指向原请求，没有 id 字段。
			const data =
				event.data && typeof event.data === "object"
					? (event.data as { requestID?: string; id?: string })
					: undefined;
			const id = data?.requestID ?? data?.id;
			if (id) setPermissions((current) => current.filter((item) => item.id !== id));
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

	const resync = () => {
		void refreshNodes();
		setSessionsRefresh((value) => value + 1);
		setMessagesRefresh((value) => value + 1);
		setPermissionsRefresh((value) => value + 1);
	};

	const { connected } = useControlEvents(handleEvent, resync);

	const selectNode = (nodeID: string) => {
		setSelectedNodeID(nodeID);
		setSelectedSessionID(null);
		setMessages([]);
		setPermissions([]);
	};

	const create = async (directory: string) => {
		if (!selectedNodeID) throw new Error("Select a connected node first");
		const session = await createSession(selectedNodeID, directory);
		setSelectedSessionID(session.id);
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
		setMessagesRefresh((value) => value + 1);
	};

	const refresh = () => {
		setSessionsRefresh((value) => value + 1);
		setMessagesRefresh((value) => value + 1);
		setPermissionsRefresh((value) => value + 1);
	};

	const topError = nodesError || sessionsError || messagesError;

	return (
		<div className="flex h-dvh overflow-hidden">
			<NodeSidebar
				nodes={nodes}
				selectedNodeID={selectedNodeID}
				onSelectNode={selectNode}
				sessions={sessions}
				selectedSessionID={selectedSessionID}
				onSelectSession={setSelectedSessionID}
				onCreateSession={create}
				onRefresh={refresh}
				loadingSessions={sessionsLoading}
			/>
			<main className="flex min-w-0 flex-1 flex-col">
				{nodesLoading ? (
					<div className="flex h-14 items-center border-b px-4 text-sm text-muted-foreground">
						Loading control plane…
					</div>
				) : null}
				{topError ? (
					<div className="border-b border-destructive/20 bg-destructive/5 px-4 py-2 text-xs text-destructive">
						{topError}
					</div>
				) : null}
				<SessionPanel
					node={selectedNode}
					session={selectedSession}
					messages={messages}
					permissions={permissions}
					connected={connected}
					loadingMessages={messagesLoading}
					sending={sending}
					interrupting={interrupting}
					onSend={send}
					onInterrupt={interrupt}
					onResolvePermission={resolvePermission}
					onRefresh={refresh}
				/>
			</main>
		</div>
	);
}
