import type { Message, PendingPermission, Session } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import {
	ActivityIndicator,
	FlatList,
	Modal,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";

import { sessionRoute } from "../../../src/config";
import { findProjectForSession } from "../../../src/projects/model";
import { runSend } from "../../../src/session/send-flow";
import { useSession } from "../../../src/session/use-session";
import { selectSiblingSessions } from "../../../src/sessions/select";
import { useNodeSessions } from "../../../src/sessions/use-node-sessions";
import { formatRelativeTime } from "../../../src/sidebar/model";
import { useAppShell, useScreenTitle } from "../../../src/ui/app-shell-context";
import { GlobalComposer } from "../../../src/ui/global-composer";
import { ScreenTopBar } from "../../../src/ui/top-bar";

function param(value: string | string[] | undefined): string {
	return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

// messageText 优先用聚合文本；部分消息只带 parts，需要现场拼。
function messageText(message: Message): string {
	if (message.text) return message.text;
	if (!message.parts) return "";
	return message.parts
		.map((part) => (part.type === "text" ? (part.text ?? "") : ""))
		.filter((text) => text !== "")
		.join("\n");
}

function MessageRow({ message }: { message: Message }) {
	const text = messageText(message);
	const isUser = message.role === "user";
	return (
		<View style={[styles.message, isUser ? styles.userMessage : styles.assistantMessage]}>
			<Text style={styles.messageRole}>{message.role}</Text>
			{text ? (
				<Text style={styles.messageText} numberOfLines={40}>
					{text}
				</Text>
			) : (
				<Text style={styles.messagePlaceholder} numberOfLines={2}>
					{message.error?.message ?? JSON.stringify(message.parts ?? []).slice(0, 120)}
				</Text>
			)}
		</View>
	);
}

function ApprovalCard({
	permission,
	onReply,
}: {
	permission: PendingPermission;
	onReply: (decision: "once" | "always" | "reject", message?: string) => Promise<void>;
}) {
	const [rejecting, setRejecting] = useState(false);
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const respond = async (decision: "once" | "always" | "reject") => {
		setBusy(true);
		setError(null);
		try {
			await onReply(decision, decision === "reject" ? reason.trim() || undefined : undefined);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "failed to reply");
		} finally {
			setBusy(false);
		}
	};

	return (
		<View style={styles.approval}>
			<Text style={styles.approvalTitle}>Approval required</Text>
			<Text style={styles.approvalAction}>{permission.action || permission.id}</Text>
			{permission.message ? <Text style={styles.approvalMessage}>{permission.message}</Text> : null}
			{permission.resources.length > 0 ? (
				<View style={styles.resources}>
					{permission.resources.slice(0, 6).map((resource) => (
						<Text key={resource} style={styles.resource} numberOfLines={2}>
							{resource}
						</Text>
					))}
				</View>
			) : null}

			{rejecting ? (
				<TextInput
					style={styles.input}
					value={reason}
					onChangeText={setReason}
					placeholder="Reason (optional)"
					editable={!busy}
				/>
			) : null}

			{error ? <Text style={styles.error}>{error}</Text> : null}

			<View style={styles.approvalButtons}>
				<Pressable
					style={[styles.primaryButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => void respond("once")}
				>
					<Text style={styles.primaryButtonText}>Allow once</Text>
				</Pressable>
				<Pressable
					style={[styles.secondaryButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => void respond("always")}
				>
					<Text style={styles.secondaryButtonText}>Always</Text>
				</Pressable>
				<Pressable
					style={[styles.rejectButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => {
						if (rejecting) {
							void respond("reject");
						} else {
							setRejecting(true);
						}
					}}
				>
					<Text style={styles.rejectButtonText}>{rejecting ? "Reject" : "Reject…"}</Text>
				</Pressable>
			</View>
		</View>
	);
}

function SessionSwitcher({
	sessions,
	currentID,
	loading,
	onPick,
	onClose,
}: {
	sessions: Session[];
	currentID: string;
	loading: boolean;
	onPick: (id: string) => void;
	onClose: () => void;
}) {
	return (
		<Pressable style={styles.backdrop} onPress={onClose}>
			<View style={styles.sheet}>
				<Text style={styles.sheetTitle}>切换会话</Text>
				<FlatList
					data={sessions}
					keyExtractor={(item) => item.id}
					renderItem={({ item }) => (
						<Pressable style={styles.sheetItem} onPress={() => onPick(item.id)}>
							<Text
								style={[styles.sheetLabel, item.id === currentID && styles.sheetLabelActive]}
								numberOfLines={1}
							>
								{item.title || item.id}
							</Text>
							<Text style={styles.sheetTime}>{formatRelativeTime(item.updatedAt)}</Text>
						</Pressable>
					)}
					ListEmptyComponent={
						loading ? (
							<ActivityIndicator style={styles.sheetLoading} />
						) : (
							<Text style={styles.hint}>这个项目下还没有会话</Text>
						)
					}
				/>
			</View>
		</Pressable>
	);
}

export default function SessionScreen() {
	const params = useLocalSearchParams<{ node: string; id: string }>();
	const nodeID = param(params.node);
	const sessionID = param(params.id);

	const { settings, settingsLoading, projects } = useAppShell();
	const {
		messages,
		permissions,
		loading,
		error,
		refresh,
		reply,
		send,
		interrupt,
		sending,
		notFound,
	} = useSession(settings, nodeID, sessionID);

	const nodeSessions = useNodeSessions(settings, nodeID);
	const current = useMemo(
		() => nodeSessions.sessions.find((session) => session.id === sessionID) ?? null,
		[nodeSessions.sessions, sessionID],
	);
	const project = useMemo(
		() => (current ? findProjectForSession(current, projects.projects) : null),
		[current, projects.projects],
	);
	const title = current?.title || "会话";
	useScreenTitle(title);

	const siblings = useMemo(() => {
		if (!current) return [];
		// 项目还没聚合出来时退回会话自身的 projectID 与目录，至少不跨项目。
		const key =
			project?.key ??
			(current.projectID === "global"
				? `global:${current.directory ?? ""}`
				: (current.projectID ?? ""));
		const canonical = project?.canonical ?? current.directory ?? "";
		if (!key && !canonical) return [current];
		return selectSiblingSessions(nodeSessions.sessions, key, canonical);
	}, [current, project, nodeSessions.sessions]);

	const [draft, setDraft] = useState("");
	const [actionError, setActionError] = useState<string | null>(null);
	const [switcherOpen, setSwitcherOpen] = useState(false);

	useEffect(() => {
		if (switcherOpen) nodeSessions.refresh();
	}, [switcherOpen, nodeSessions.refresh]);

	const submit = async () => {
		const text = draft.trim();
		if (!text) return;
		setActionError(null);
		try {
			await runSend(
				() => send(text),
				() => setDraft(""),
			);
		} catch (caught) {
			setActionError(caught instanceof Error ? caught.message : "failed to send");
		}
	};

	const stop = async () => {
		setActionError(null);
		try {
			await interrupt();
		} catch (caught) {
			setActionError(caught instanceof Error ? caught.message : "failed to interrupt");
		}
	};

	if (settingsLoading) {
		return (
			<View style={styles.center}>
				<ActivityIndicator />
			</View>
		);
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>Not connected</Text>
				<Text style={styles.hint}>Configure the control plane in Settings first.</Text>
				<Pressable style={styles.primaryButton} onPress={() => router.push("/settings")}>
					<Text style={styles.primaryButtonText}>Open settings</Text>
				</Pressable>
			</View>
		);
	}

	if (notFound) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>会话不存在</Text>
				<Text style={styles.hint}>它可能已被删除，或这条深链已经过期。</Text>
				<Pressable style={styles.primaryButton} onPress={() => router.replace("/pending")}>
					<Text style={styles.primaryButtonText}>去待审批</Text>
				</Pressable>
				<Pressable style={styles.secondaryButton} onPress={() => router.replace("/")}>
					<Text style={styles.secondaryButtonText}>回首页</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<View style={styles.screen}>
			<ScreenTopBar
				title={title}
				onTitlePress={() => setSwitcherOpen(true)}
				extra={
					<Pressable style={styles.stopButton} onPress={() => void stop()}>
						<Text style={styles.stopText}>停止</Text>
					</Pressable>
				}
			/>

			{error ? <Text style={styles.error}>{error}</Text> : null}
			{actionError ? <Text style={styles.error}>{actionError}</Text> : null}

			<FlatList
				style={styles.messages}
				contentContainerStyle={styles.messagesContent}
				data={messages}
				keyExtractor={(message) => message.id}
				renderItem={({ item }) => <MessageRow message={item} />}
				refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
				ListHeaderComponent={
					permissions.length > 0 ? (
						<View style={styles.approvals}>
							{permissions.map((permission) => (
								<ApprovalCard
									key={permission.id}
									permission={permission}
									onReply={(decision, message) => reply(permission.id, decision, message)}
								/>
							))}
						</View>
					) : undefined
				}
				ListEmptyComponent={
					loading ? undefined : (
						<View style={styles.center}>
							<Text style={styles.hint}>No messages yet.</Text>
						</View>
					)
				}
			/>

			<GlobalComposer
				target={{
					projectKey: project?.key ?? `${nodeID}:${sessionID}`,
					projectName: title,
					nodeID,
					showNode: false,
				}}
				showChip={false}
				value={draft}
				onChangeText={setDraft}
				onSend={submit}
				sending={sending}
			/>

			<Modal
				transparent
				visible={switcherOpen}
				animationType="fade"
				onRequestClose={() => setSwitcherOpen(false)}
			>
				<SessionSwitcher
					sessions={siblings}
					currentID={sessionID}
					loading={nodeSessions.loading}
					onPick={(id) => {
						setSwitcherOpen(false);
						if (id !== sessionID) router.replace(sessionRoute(nodeID, id));
					}}
					onClose={() => setSwitcherOpen(false)}
				/>
			</Modal>
		</View>
	);
}

const styles = StyleSheet.create({
	screen: { flex: 1 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	title: { fontSize: 18, fontWeight: "600" },
	hint: { color: "#666", textAlign: "center" },
	error: { color: "#b91c1c", paddingHorizontal: 16, paddingTop: 8 },
	stopButton: { paddingHorizontal: 6, paddingVertical: 6 },
	stopText: { fontSize: 13, color: "#475569" },
	messages: { flex: 1 },
	messagesContent: { padding: 16, gap: 10 },
	approvals: { gap: 10, marginBottom: 12 },
	approval: {
		borderWidth: 1,
		borderColor: "#f59e0b",
		backgroundColor: "#fffbeb",
		borderRadius: 12,
		padding: 12,
		gap: 8,
	},
	approvalTitle: { fontSize: 14, fontWeight: "700", color: "#92400e" },
	approvalAction: { fontSize: 15, fontWeight: "600" },
	approvalMessage: { fontSize: 13, color: "#475569" },
	resources: { gap: 4 },
	resource: {
		fontFamily: "monospace",
		fontSize: 12,
		color: "#334155",
		backgroundColor: "#f1f5f9",
		borderRadius: 6,
		paddingHorizontal: 8,
		paddingVertical: 6,
	},
	approvalButtons: { flexDirection: "row", flexWrap: "wrap", gap: 8, marginTop: 4 },
	message: { borderRadius: 12, padding: 10, gap: 4 },
	userMessage: { backgroundColor: "#eff6ff" },
	assistantMessage: { backgroundColor: "#f8fafc" },
	messageRole: { fontSize: 11, fontWeight: "700", color: "#64748b", textTransform: "uppercase" },
	messageText: { fontSize: 14, lineHeight: 20 },
	messagePlaceholder: { fontSize: 12, color: "#94a3b8" },
	input: {
		borderWidth: 1,
		borderColor: "#cbd5e1",
		borderRadius: 8,
		paddingHorizontal: 10,
		paddingVertical: 8,
		fontSize: 14,
		backgroundColor: "#fff",
	},
	primaryButton: {
		backgroundColor: "#0f172a",
		borderRadius: 8,
		paddingHorizontal: 14,
		paddingVertical: 10,
		alignItems: "center",
		justifyContent: "center",
	},
	primaryButtonText: { color: "#fff", fontWeight: "600" },
	secondaryButton: {
		borderWidth: 1,
		borderColor: "#cbd5e1",
		borderRadius: 8,
		paddingHorizontal: 12,
		paddingVertical: 8,
		alignItems: "center",
	},
	secondaryButtonText: { color: "#0f172a", fontWeight: "600" },
	rejectButton: {
		borderWidth: 1,
		borderColor: "#ef4444",
		borderRadius: 8,
		paddingHorizontal: 12,
		paddingVertical: 8,
		alignItems: "center",
	},
	rejectButtonText: { color: "#b91c1c", fontWeight: "600" },
	disabled: { opacity: 0.5 },
	backdrop: { flex: 1, backgroundColor: "rgba(15, 23, 42, 0.28)", justifyContent: "flex-end" },
	sheet: {
		backgroundColor: "#fff",
		borderTopLeftRadius: 16,
		borderTopRightRadius: 16,
		padding: 16,
		gap: 8,
		maxHeight: "65%",
	},
	sheetTitle: { fontSize: 16, fontWeight: "600", marginBottom: 4 },
	sheetItem: {
		flexDirection: "row",
		alignItems: "center",
		justifyContent: "space-between",
		gap: 12,
		paddingVertical: 12,
		borderBottomWidth: 1,
		borderBottomColor: "#f1f5f9",
	},
	sheetLabel: { flex: 1, fontSize: 15, color: "#0f172a" },
	sheetLabelActive: { fontWeight: "700", color: "#2563eb" },
	sheetTime: { fontSize: 11, color: "#94a3b8" },
	sheetLoading: { paddingVertical: 24 },
});
