import type { Message, PendingPermission } from "@lework/api";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { useState } from "react";
import {
	ActivityIndicator,
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";

import { useSettings } from "../../../src/settings/use-settings";
import { useSession } from "../../../src/session/use-session";

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

export default function SessionScreen() {
	const params = useLocalSearchParams<{ node: string; id: string }>();
	const nodeID = param(params.node);
	const sessionID = param(params.id);

	const { settings, loading: settingsLoading } = useSettings();
	const { messages, permissions, loading, error, refresh, reply, send, interrupt, connected } =
		useSession(settings, nodeID, sessionID);
	const [draft, setDraft] = useState("");
	const [sending, setSending] = useState(false);
	const [actionError, setActionError] = useState<string | null>(null);

	const submit = async () => {
		const text = draft.trim();
		if (!text) return;
		setSending(true);
		setActionError(null);
		try {
			await send(text);
			setDraft("");
		} catch (caught) {
			setActionError(caught instanceof Error ? caught.message : "failed to send");
		} finally {
			setSending(false);
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

	return (
		<View style={styles.screen}>
			<Stack.Screen options={{ title: sessionID }} />
			<View style={styles.toolbar}>
				<Text style={styles.toolbarMeta} numberOfLines={1}>
					{nodeID} · {connected ? "live" : "offline"}
				</Text>
				<Pressable style={styles.secondaryButton} onPress={() => void stop()}>
					<Text style={styles.secondaryButtonText}>Interrupt</Text>
				</Pressable>
			</View>

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

			<View style={styles.composer}>
				<TextInput
					style={styles.composerInput}
					value={draft}
					onChangeText={setDraft}
					placeholder="Reply…"
					multiline
					editable={!sending}
				/>
				<Pressable
					style={[styles.primaryButton, (sending || !draft.trim()) && styles.disabled]}
					disabled={sending || !draft.trim()}
					onPress={() => void submit()}
				>
					{sending ? <ActivityIndicator color="#fff" /> : <Text style={styles.primaryButtonText}>Send</Text>}
				</Pressable>
			</View>
		</View>
	);
}

const styles = StyleSheet.create({
	screen: { flex: 1 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	title: { fontSize: 18, fontWeight: "600" },
	hint: { color: "#666", textAlign: "center" },
	error: { color: "#b91c1c", paddingHorizontal: 16, paddingTop: 8 },
	toolbar: {
		flexDirection: "row",
		alignItems: "center",
		justifyContent: "space-between",
		paddingHorizontal: 16,
		paddingVertical: 8,
		gap: 8,
	},
	toolbarMeta: { flex: 1, fontSize: 12, color: "#64748b" },
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
	composer: {
		flexDirection: "row",
		alignItems: "flex-end",
		gap: 8,
		padding: 12,
		borderTopWidth: 1,
		borderTopColor: "#e2e8f0",
	},
	composerInput: {
		flex: 1,
		maxHeight: 120,
		borderWidth: 1,
		borderColor: "#cbd5e1",
		borderRadius: 8,
		paddingHorizontal: 10,
		paddingVertical: 8,
		fontSize: 14,
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
});
