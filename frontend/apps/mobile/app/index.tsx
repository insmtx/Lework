import { router } from "expo-router";
import { type ComponentRef, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	Alert,
	KeyboardAvoidingView,
	Modal,
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
import { nodeColorSwatch } from "../src/nodes/colors";
import {
	nodeCanCreateProject,
	nodeCanCreateSession,
	projectDirectoryOnNode,
	projectDisplayName,
	projectNameError,
	projectsOnNode,
	resolveSelectionTarget,
	type TaskSelection,
} from "../src/projects/model";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { ComposerBar, ComposerChip } from "../src/ui/composer-bar";
import { BrandSpinner } from "../src/ui/loading";
import { OptionSheet, type OptionSheetItem } from "../src/ui/option-sheet";
import { DrawerButton } from "../src/ui/top-bar";

// 首页草稿不跟随项目切换：用户还没决定发给谁时，字不能丢。
const HOME_DRAFT_SCOPE = { projectKey: "home", nodeID: "home" };

// 「我的目录」在项目弹层里的选项键；用前缀避免和真实项目聚合键冲突。
const INBOX_OPTION_KEY = "__inbox__";
// 新建项目入口与「待建项目」的选项键。
const NEW_PROJECT_OPTION_KEY = "__new_project__";
const PENDING_PROJECT_OPTION_KEY = "__pending_project__";

/**
 * 首页只做一件事：直接说任务。
 *
 * 没有标题、路径、会话列表或节点信息——导航在侧边栏，历史在项目页。
 * 默认自动挑节点（主节点 → 最近在线）；输入框上方的 chip 允许用户临时
 * 指定电脑与项目，选择只在本次停留有效。
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
	// 显式选择只在本次停留有效，不写偏好；失效时由解析函数自动回退。
	const [selection, setSelection] = useState<TaskSelection | null>(null);
	const [nodeSheetOpen, setNodeSheetOpen] = useState(false);
	const [projectSheetOpen, setProjectSheetOpen] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const [newProjectName, setNewProjectName] = useState("");
	const [creating, setCreating] = useState(false);
	const [createError, setCreateError] = useState<string | null>(null);

	// 只把「在线且能新建会话」的节点当成可用目标：离线或旧 agent 不应该
	// 被自动选中，选择器里也置灰。
	const usableNodeIDs = useMemo(
		() => projects.nodes.filter((node) => nodeCanCreateSession(node)).map((node) => node.id),
		[projects.nodes],
	);
	const target = useMemo(
		() =>
			resolveSelectionTarget(
				selection,
				projects.projects,
				projects.dirByNode,
				usableNodeIDs,
				projects.inbox,
			),
		[selection, projects.projects, projects.dirByNode, usableNodeIDs, projects.inbox],
	);

	const activeNode = useMemo(
		() => projects.nodes.find((node) => node.id === target.nodeID) ?? null,
		[projects.nodes, target.nodeID],
	);
	const activeProject = useMemo(
		() =>
			target.projectKey
				? (projects.projects.find((project) => project.key === target.projectKey) ?? null)
				: null,
		[projects.projects, target.projectKey],
	);

	const canPickTarget = !settingsLoading && Boolean(settings?.sessionToken);
	const createRoot = activeNode?.workspace_roots?.[0] ?? "";
	const canCreateProject =
		canPickTarget &&
		target.nodeID !== null &&
		activeNode !== null &&
		createRoot !== "" &&
		nodeCanCreateProject(activeNode);
	const nodeLabel = activeNode ? activeNode.name || activeNode.id : "未选择";
	const projectLabel =
		target.pendingName ?? (activeProject ? projectDisplayName(activeProject) : "我的目录");

	const nodeItems = useMemo<OptionSheetItem[]>(
		() =>
			projects.nodes.map((node) => {
				const usable = nodeCanCreateSession(node);
				return {
					key: node.id,
					label: node.name || node.id,
					detail: projects.dirByNode[node.id],
					status: !node.connected ? "离线" : usable ? "在线" : "不支持新建",
					swatch: nodeColorSwatch(node.color) ?? undefined,
					tone: usable ? "normal" : "muted",
					selected: node.id === target.nodeID,
					disabled: !usable,
				};
			}),
		[projects.nodes, projects.dirByNode, target.nodeID],
	);

	const projectItems = useMemo<OptionSheetItem[]>(() => {
		if (!target.nodeID) return [];
		const inboxDir = projects.dirByNode[target.nodeID] ?? "";
		const items: OptionSheetItem[] = [];
		// 刚创建、OpenCode 还没登记的项目置顶显示，避免选中态在刷新后消失。
		if (target.pendingName) {
			items.push({
				key: PENDING_PROJECT_OPTION_KEY,
				label: target.pendingName,
				detail: target.directory || "刚刚创建",
				selected: true,
			});
		}
		items.push({
			key: INBOX_OPTION_KEY,
			label: "我的目录",
			detail: inboxDir || "没有可用目录",
			selected: target.projectKey === null && !target.pendingName,
			disabled: inboxDir === "",
		});
		for (const project of projectsOnNode(projects.projects, target.nodeID)) {
			items.push({
				key: project.key,
				label: projectDisplayName(project),
				detail: projectDirectoryOnNode(project, target.nodeID),
				selected: target.projectKey === project.key,
			});
		}
		if (canCreateProject) {
			items.push({
				key: NEW_PROJECT_OPTION_KEY,
				label: "＋ 新建项目",
				detail: `在 ${createRoot} 下创建空目录`,
			});
		}
		return items;
	}, [
		projects.projects,
		projects.dirByNode,
		target.nodeID,
		target.projectKey,
		target.pendingName,
		target.directory,
		canCreateProject,
		createRoot,
	]);

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

	const pickNode = useCallback(
		(nodeID: string) => {
			setNodeSheetOpen(false);
			const currentProjectKey = target.projectKey;
			// 原项目在新电脑上也有时保留，否则回落到那台电脑的「我的目录」。
			const keep =
				currentProjectKey !== null &&
				projectsOnNode(projects.projects, nodeID).some(
					(project) => project.key === currentProjectKey,
				);
			setSelection({ nodeID, projectKey: keep ? currentProjectKey : null });
		},
		[target.projectKey, projects.projects],
	);

	const pickProject = useCallback(
		(key: string) => {
			setProjectSheetOpen(false);
			if (!target.nodeID) return;
			if (key === NEW_PROJECT_OPTION_KEY) {
				setNewProjectName("");
				setCreateError(null);
				setCreateOpen(true);
				return;
			}
			if (key === PENDING_PROJECT_OPTION_KEY) return;
			setSelection({
				nodeID: target.nodeID,
				projectKey: key === INBOX_OPTION_KEY ? null : key,
			});
		},
		[target.nodeID],
	);

	const closeCreate = useCallback(() => {
		if (creating) return;
		setCreateOpen(false);
		setCreateError(null);
	}, [creating]);

	const createProject = useCallback(async () => {
		const nodeID = target.nodeID;
		const name = newProjectName.trim();
		if (!nodeID) return;
		const invalid = projectNameError(name);
		if (invalid) {
			setCreateError(invalid);
			return;
		}

		setCreating(true);
		setCreateError(null);
		try {
			const created = await projects.createProject(nodeID, name);
			// OpenCode 要等第一条会话才登记项目，先把它作为待建目标选中。
			setSelection({
				nodeID,
				projectKey: null,
				pendingProject: { name, directory: created.canonical },
			});
			setCreateOpen(false);
			setNewProjectName("");
		} catch (caught) {
			setCreateError(caught instanceof Error ? caught.message : "创建失败，请稍后重试");
		} finally {
			setCreating(false);
		}
	}, [target.nodeID, newProjectName, projects]);

	const send = useCallback(async () => {
		const text = draft.trim();
		const nodeID = target.nodeID;
		if (!settings?.sessionToken || !nodeID || target.directory === "" || text === "") return;

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

	const disabledReason = !settings?.sessionToken
		? "先登录账号"
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
					<ComposerBar>
						<ComposerChip
							label="电脑"
							value={nodeLabel}
							status={activeNode ? (activeNode.connected ? "online" : "offline") : "none"}
							accentColor={nodeColorSwatch(activeNode?.color) ?? undefined}
							disabled={!canPickTarget}
							onPress={() => setNodeSheetOpen(true)}
						/>
						<ComposerChip
							label="项目"
							value={projectLabel}
							disabled={!canPickTarget || target.nodeID === null}
							onPress={() => setProjectSheetOpen(true)}
						/>
					</ComposerBar>
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
								<BrandSpinner color={theme.colors.actionText} />
							) : (
								<Text style={styles.sendText}>发送</Text>
							)}
						</Pressable>
					</View>
				</View>

				{!settingsLoading && !settings?.sessionToken ? (
					<Pressable style={styles.link} onPress={() => router.push("/login")}>
						<Text style={styles.linkText}>登录以查看电脑</Text>
					</Pressable>
				) : null}
			</KeyboardAvoidingView>

			<OptionSheet
				visible={nodeSheetOpen}
				title="选择电脑"
				subtitle="任务会发送到这台电脑上"
				items={nodeItems}
				emptyText="还没有关联电脑，先在设置里配对节点。"
				onSelect={pickNode}
				onClose={() => setNodeSheetOpen(false)}
			/>
			<OptionSheet
				visible={projectSheetOpen}
				title="选择项目"
				subtitle={nodeLabel}
				items={projectItems}
				emptyText="这台电脑上还没有目录，先选「我的目录」。"
				onSelect={pickProject}
				onClose={() => setProjectSheetOpen(false)}
			/>

			<Modal transparent visible={createOpen} animationType="fade" onRequestClose={closeCreate}>
				<View style={styles.modalBackdrop}>
					<Pressable style={StyleSheet.absoluteFill} onPress={closeCreate} />
					<View style={[styles.modalCard, { paddingBottom: Math.max(insets.bottom, 16) }]}>
						<Text style={styles.modalTitle}>新建项目</Text>
						<Text style={styles.modalHint}>
							在 {nodeLabel} 的 {createRoot || "工作目录"} 下创建一个空目录
						</Text>
						<TextInput
							style={styles.modalInput}
							value={newProjectName}
							onChangeText={(text) => {
								setNewProjectName(text);
								setCreateError(null);
							}}
							onSubmitEditing={() => void createProject()}
							autoFocus
							placeholder="项目名，例如 my-app"
							placeholderTextColor={theme.colors.placeholder}
							selectionColor={theme.colors.accent}
						/>
						<Text style={styles.modalPreview}>
							{createRoot}/{newProjectName.trim() || "…"}
						</Text>
						{createError ? <Text style={styles.modalError}>{createError}</Text> : null}
						<View style={styles.modalActions}>
							<Pressable disabled={creating} onPress={closeCreate} style={styles.modalCancel}>
								<Text style={styles.modalCancelText}>取消</Text>
							</Pressable>
							<Pressable
								disabled={creating}
								onPress={() => void createProject()}
								style={[styles.modalConfirm, creating && styles.modalConfirmDisabled]}
							>
								{creating ? (
									<BrandSpinner color={theme.colors.actionText} />
								) : (
									<Text style={styles.modalConfirmText}>创建</Text>
								)}
							</Pressable>
						</View>
					</View>
				</View>
			</Modal>
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
		modalBackdrop: { flex: 1, backgroundColor: colors.modalBackdrop, justifyContent: "flex-end" },
		modalCard: {
			backgroundColor: colors.surface,
			borderTopLeftRadius: 16,
			borderTopRightRadius: 16,
			padding: 16,
			gap: 10,
		},
		modalTitle: { fontSize: 16, fontWeight: "600", color: colors.text },
		modalHint: { fontSize: 12, color: colors.textSecondary },
		modalInput: {
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: 10,
			paddingHorizontal: 12,
			paddingVertical: 10,
			fontSize: 16,
			color: colors.text,
			backgroundColor: colors.surfaceMuted,
		},
		modalPreview: { fontSize: 12, color: colors.textMuted },
		modalError: { fontSize: 12, color: colors.danger },
		modalActions: { flexDirection: "row", justifyContent: "flex-end", gap: 12, marginTop: 4 },
		modalCancel: { paddingHorizontal: 16, paddingVertical: 10, borderRadius: 10 },
		modalCancelText: { fontSize: 14, color: colors.textSecondary },
		modalConfirm: {
			minWidth: 80,
			alignItems: "center",
			justifyContent: "center",
			paddingHorizontal: 16,
			paddingVertical: 10,
			borderRadius: 10,
			backgroundColor: colors.action,
		},
		modalConfirmDisabled: { opacity: 0.35 },
		modalConfirmText: { fontSize: 14, color: colors.actionText, fontWeight: "600" },
	});
}
