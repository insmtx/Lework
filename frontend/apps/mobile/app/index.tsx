import { router } from "expo-router";
import { type ComponentRef, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	ActivityIndicator,
	Alert,
	KeyboardAvoidingView,
	Platform,
	Pressable,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { createClient } from "../src/api/client";
import { createSecureDraftStore } from "../src/composer/secure-draft";
import { newIdempotencyKey, sendToProject } from "../src/composer/send";
import { sessionRoute } from "../src/config";
import { resolveTaskTarget } from "../src/projects/model";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { DrawerButton } from "../src/ui/top-bar";

// 首页草稿不跟随项目切换：用户还没决定发给谁时，字不能丢。
const HOME_DRAFT_SCOPE = { projectKey: "home", nodeID: "home" };

/**
 * 首页只做一件事：直接说任务。
 *
 * 没有标题、路径、会话列表或节点信息——导航在侧边栏，历史在项目页。
 * 输入框提交时自动挑节点（主节点 → 最近在线），用户不需要先选机器。
 */
export default function HomeScreen() {
	useScreenTitle("新任务");
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, projects, newTaskSeq } = useAppShell();
	const insets = useSafeAreaInsets();
	const store = useMemo(() => createSecureDraftStore(), []);
	const inputRef = useRef<ComponentRef<typeof TextInput>>(null);

	// 草稿固定在「首页」scope：切项目或节点都不该让用户刚打的字消失。
	const [draft, setDraft] = useState("");
	const [sending, setSending] = useState(false);

	const onlineNodeIDs = useMemo(
		() => projects.nodes.filter((node) => node.connected).map((node) => node.id),
		[projects.nodes],
	);
	const target = useMemo(
		() => resolveTaskTarget(projects.inbox, projects.dirByNode, onlineNodeIDs),
		[projects.inbox, projects.dirByNode, onlineNodeIDs],
	);

	useEffect(() => {
		let cancelled = false;
		void store
			.load(HOME_DRAFT_SCOPE)
			.then((value) => {
				if (!cancelled) setDraft(value);
			})
			.catch(() => {
				// 读不到草稿不是致命错误：输入框留空即可。
			});
		return () => {
			cancelled = true;
		};
	}, [store]);

	useEffect(() => {
		if (newTaskSeq === 0) return;
		const timer = setTimeout(() => inputRef.current?.focus(), 60);
		return () => clearTimeout(timer);
	}, [newTaskSeq]);

	const changeText = useCallback(
		(text: string) => {
			setDraft(text);
			void store.save(HOME_DRAFT_SCOPE, text).catch(() => {
				// 草稿落盘失败只影响下次恢复，不能阻塞输入。
			});
		},
		[store],
	);

	const send = useCallback(async () => {
		const text = draft.trim();
		const nodeID = target.nodeID;
		if (!settings || !nodeID || target.directory === "" || text === "") return;

		setSending(true);
		try {
			const session = await sendToProject(
				createClient(settings),
				nodeID,
				target.directory,
				text,
				newIdempotencyKey(nodeID, "new"),
			);
			await store.save(HOME_DRAFT_SCOPE, "");
			setDraft("");
			projects.refresh();
			router.push(sessionRoute(nodeID, session.id));
		} catch (caught) {
			// 发送失败保留草稿：用户重试时不该重新打一遍。
			Alert.alert("发送失败", caught instanceof Error ? caught.message : "请稍后重试");
		} finally {
			setSending(false);
		}
	}, [settings, target, draft, store, projects]);

	const disabledReason = !settings
		? "先连接控制面"
		: target.reason === "no-dir"
			? "还没有可用的任务目录"
			: target.reason === "no-node"
				? "没有在线节点"
				: undefined;
	const canSend =
		!settingsLoading && disabledReason === undefined && !sending && draft.trim() !== "";

	return (
		<View style={styles.screen}>
			<View style={[styles.menu, { top: insets.top + 4 }]}>
				<DrawerButton />
			</View>

			<KeyboardAvoidingView
				style={styles.center}
				behavior={Platform.OS === "ios" ? "padding" : "height"}
			>
				<View style={styles.card}>
					<TextInput
						ref={inputRef}
						style={styles.input}
						value={draft}
						onChangeText={changeText}
						editable={!settingsLoading && disabledReason === undefined}
						multiline
						placeholder="现在要做什么？"
						placeholderTextColor={theme.colors.placeholder}
						selectionColor={theme.colors.accent}
						textAlignVertical="top"
					/>
					<View style={styles.footer}>
						<Text style={styles.reason}>{disabledReason ?? "回车换行"}</Text>
						<Pressable
							disabled={!canSend}
							onPress={() => void send()}
							style={[styles.send, !canSend && styles.sendDisabled]}
						>
							{sending ? (
								<ActivityIndicator color={theme.colors.actionText} />
							) : (
								<Text style={styles.sendText}>发送</Text>
							)}
						</Pressable>
					</View>
				</View>

				{!settingsLoading && !settings ? (
					<Pressable style={styles.link} onPress={() => router.push("/settings")}>
						<Text style={styles.linkText}>去设置连接</Text>
					</Pressable>
				) : null}
			</KeyboardAvoidingView>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		screen: { flex: 1, backgroundColor: colors.background },
		menu: { position: "absolute", left: 8, zIndex: 1 },
		center: { flex: 1, justifyContent: "center", paddingHorizontal: 20, paddingVertical: 32 },
		card: {
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: 20,
			backgroundColor: colors.surfaceMuted,
			padding: 16,
			gap: 12,
		},
		input: {
			minHeight: 96,
			maxHeight: 220,
			fontSize: 18,
			lineHeight: 26,
			color: colors.text,
			padding: 0,
		},
		footer: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 12,
		},
		reason: { flex: 1, fontSize: 12, color: colors.textMuted },
		send: {
			height: 38,
			minWidth: 72,
			paddingHorizontal: 18,
			borderRadius: 19,
			backgroundColor: colors.action,
			alignItems: "center",
			justifyContent: "center",
		},
		sendDisabled: { opacity: 0.35 },
		sendText: { color: colors.actionText, fontWeight: "600" },
		link: { alignSelf: "center", marginTop: 16, padding: 8 },
		linkText: { fontSize: 14, color: colors.accent },
	});
}
