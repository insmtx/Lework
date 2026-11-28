import { router, useFocusEffect } from "expo-router";
import { type ComponentRef, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	Alert,
	KeyboardAvoidingView,
	Modal,
	Platform,
	Pressable,
	ScrollView,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { createClient } from "../src/api/client";
import { type PickedAttachment, pickAttachments } from "../src/composer/attachments";
import { createSecureDraftStore } from "../src/composer/secure-draft";
import { newIdempotencyKey, sendToProject } from "../src/composer/send";
import { sessionRoute } from "../src/config";
import { nodeColorSwatch } from "../src/nodes/colors";
import {
	findProjectForSession,
	nodeCanCreateProject,
	nodeCanCreateSession,
	projectDirectoryOnNode,
	projectDisplayName,
	projectNameError,
	projectsOnNode,
	resolveSelectionTarget,
	type TaskSelection,
} from "../src/projects/model";
import { isNearBottom } from "../src/session/scroll";
import { nextRecentLimit, recentProjectOptions } from "../src/sessions/select";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { ComposerBar, ComposerChip } from "../src/ui/composer-bar";
import { BrandSpinner } from "../src/ui/loading";
import { OptionSheet, type OptionSheetItem } from "../src/ui/option-sheet";
import { SessionRow } from "../src/ui/session-row";
import { DrawerButton } from "../src/ui/top-bar";

// 首页草稿不跟随项目切换：用户还没决定发给谁时，字不能丢。
const HOME_DRAFT_SCOPE = { projectKey: "home", nodeID: "home" };

// 首页先露 5 条最近会话，主动上滑再续 5 条：一次铺满会把输入框挤出屏幕。
const HOME_RECENT_PAGE = 5;

// 「我的目录」在项目弹层里的选项键；用前缀避免和真实项目聚合键冲突。
const INBOX_OPTION_KEY = "__inbox__";
// 新建项目入口与「待建项目」的选项键。
const NEW_PROJECT_OPTION_KEY = "__new_project__";
const PENDING_PROJECT_OPTION_KEY = "__pending_project__";
// 筛选弹层里「全部项目」的选项键；用前缀避免和真实项目聚合键冲突。
const ALL_PROJECTS_OPTION_KEY = "__all_projects__";

/**
 * 首页只做一件事：直接说任务。
 *
 * 没有标题与路径——导航在侧边栏，完整历史在项目页；输入框下方只留几条最近
 * 会话作为继续入口。默认自动挑节点（主节点 → 最近在线）；输入框上方的 chip
 * 允许用户临时指定电脑与项目，选择只在本次停留有效。
 */
export default function HomeScreen() {
	useScreenTitle("新任务");
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const {
		settings,
		settingsLoading,
		projects,
		newTaskSeq,
		recentSessions,
		sessionState,
		refreshSidebar,
	} = useAppShell();
	const insets = useSafeAreaInsets();
	const store = useMemo(() => createSecureDraftStore(), []);
	const inputRef = useRef<ComponentRef<typeof TextInput>>(null);

	// 草稿固定在「首页」scope：切项目或节点都不该让用户刚打的字消失。
	const [draft, setDraft] = useState("");
	const [attachments, setAttachments] = useState<PickedAttachment[]>([]);
	const [sending, setSending] = useState(false);
	// 首页最近会话的露出条数，每次上滑加一页；手势内只加载一次。
	const [recentLimit, setRecentLimit] = useState(HOME_RECENT_PAGE);
	const moreLoadedInGesture = useRef(false);
	const [recentFilterOpen, setRecentFilterOpen] = useState(false);
	const [recentFilterKey, setRecentFilterKey] = useState<string | null>(null);
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

	const nodeNameByID = useMemo(
		() => Object.fromEntries(projects.nodes.map((node) => [node.id, node.name || node.id])),
		[projects.nodes],
	);
	const nodeColorByID = useMemo(
		() => Object.fromEntries(projects.nodes.map((node) => [node.id, node.color ?? ""])),
		[projects.nodes],
	);
	const recentRows = useMemo(
		() =>
			recentSessions.map((ref) => {
				const project = findProjectForSession(ref.session, projects.projects);
				return {
					key: `${ref.nodeID}:${ref.session.id}`,
					ref,
					projectKey: project ? project.key : null,
					projectName: project ? projectDisplayName(project) : "",
				};
			}),
		[recentSessions, projects.projects],
	);
	const recentProjectItems = useMemo<OptionSheetItem[]>(() => {
		const options = recentProjectOptions(recentRows);
		// 选中项目已不在最近会话里时自动回到「全部」，避免卡在空列表。
		const activeKey =
			recentFilterKey && options.some((item) => item.key === recentFilterKey)
				? recentFilterKey
				: null;
		return [
			{ key: ALL_PROJECTS_OPTION_KEY, label: "全部项目", selected: activeKey === null },
			...options.map((item) => ({
				key: item.key,
				label: item.label,
				selected: item.key === activeKey,
			})),
		];
	}, [recentRows, recentFilterKey]);
	const activeFilterKey = useMemo(() => {
		const selected = recentProjectItems.find(
			(item) => item.selected && item.key !== ALL_PROJECTS_OPTION_KEY,
		);
		return selected ? selected.key : null;
	}, [recentProjectItems]);
	const filteredRecentRows = useMemo(
		() =>
			activeFilterKey ? recentRows.filter((row) => row.projectKey === activeFilterKey) : recentRows,
		[recentRows, activeFilterKey],
	);
	const visibleRecentRows = filteredRecentRows.slice(0, recentLimit);
	// 换筛选条件后回到第一页，避免沿用上一个项目的露出条数。
	// biome-ignore lint/correctness/useExhaustiveDependencies: 只在筛选键变化时重置页数
	useEffect(() => {
		setRecentLimit(HOME_RECENT_PAGE);
	}, [activeFilterKey]);

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

	// 回到首页时刷新会话快照：状态由事件流实时更新，列表成员与标题靠这次拉取。
	useFocusEffect(
		useCallback(() => {
			if (!settings?.sessionToken) return;
			refreshSidebar();
		}, [settings?.sessionToken, refreshSidebar]),
	);

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
		if (
			!settings?.sessionToken ||
			!nodeID ||
			target.directory === "" ||
			(text === "" && attachments.length === 0)
		)
			return;

		setSending(true);
		try {
			const session = await sendToProject(
				createClient(settings),
				nodeID,
				target.directory,
				text,
				newIdempotencyKey(nodeID, "new"),
				attachments,
			);
			await store.save(HOME_DRAFT_SCOPE, "");
			setDraft("");
			setAttachments([]);
			projects.refresh();
			router.push(sessionRoute(nodeID, session.id));
		} catch (caught) {
			// 发送失败保留草稿与附件：用户重试时不该重新打一遍、重新选一遍。
			Alert.alert("发送失败", caught instanceof Error ? caught.message : "请稍后重试");
		} finally {
			setSending(false);
		}
	}, [settings, target, draft, attachments, store, projects]);

	const pickFiles = useCallback(async () => {
		const picked = await pickAttachments();
		setAttachments((current) => [...current, ...picked]);
	}, []);

	const removeAttachment = useCallback((index: number) => {
		setAttachments((current) => current.filter((_, i) => i !== index));
	}, []);

	const disabledReason = !settings?.sessionToken
		? "先登录账号"
		: target.reason === "no-dir"
			? "还没有可用的任务目录"
			: target.reason === "no-node"
				? "没有在线节点"
				: undefined;
	const canSend =
		!settingsLoading &&
		disabledReason === undefined &&
		!sending &&
		(draft.trim() !== "" || attachments.length > 0);

	return (
		<View style={styles.screen}>
			<View style={[styles.menu, { top: insets.top + 4 }]}>
				<DrawerButton />
			</View>

			<KeyboardAvoidingView
				style={styles.center}
				behavior={Platform.OS === "ios" ? "padding" : "height"}
			>
				<ScrollView
					contentContainerStyle={styles.centerContent}
					keyboardShouldPersistTaps="handled"
					scrollEventThrottle={16}
					onScrollBeginDrag={() => {
						moreLoadedInGesture.current = false;
					}}
					onScroll={(event) => {
						// 一次上滑只续一页：加载后内容仍贴着底部，不能顺着持续滚动全灌进来。
						if (moreLoadedInGesture.current) return;
						const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
						const nearBottom = isNearBottom({
							offsetY: contentOffset.y,
							contentHeight: contentSize.height,
							viewportHeight: layoutMeasurement.height,
						});
						if (!nearBottom) return;
						moreLoadedInGesture.current = true;
						setRecentLimit((limit) =>
							nextRecentLimit(limit, filteredRecentRows.length, HOME_RECENT_PAGE),
						);
					}}
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
						{attachments.length > 0 ? (
							<View style={styles.attachments}>
								{attachments.map((attachment, index) => (
									<View key={`${attachment.name}:${attachment.uri}`} style={styles.attachment}>
										<Text style={styles.attachmentName} numberOfLines={1}>
											{attachment.name}
										</Text>
										<Pressable
											accessibilityLabel={`移除 ${attachment.name}`}
											onPress={() => removeAttachment(index)}
											style={styles.attachmentRemove}
										>
											<Text style={styles.attachmentRemoveText}>×</Text>
										</Pressable>
									</View>
								))}
							</View>
						) : null}
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
							<Pressable
								disabled={settingsLoading || disabledReason !== undefined}
								onPress={() => void pickFiles()}
								style={[
									styles.attach,
									(settingsLoading || disabledReason !== undefined) && styles.sendDisabled,
								]}
								accessibilityLabel="添加附件"
							>
								<Text style={styles.attachText}>＋</Text>
							</Pressable>
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

					{recentRows.length > 0 ? (
						<View style={styles.recent}>
							<View style={styles.recentHeader}>
								<Text style={styles.recentTitle}>最近会话</Text>
								<Pressable style={styles.recentFilter} onPress={() => setRecentFilterOpen(true)}>
									<Text style={styles.recentFilterText} numberOfLines={1}>
										{recentProjectItems.find((item) => item.selected)?.label ?? "全部项目"}
									</Text>
									<Text style={styles.recentFilterCaret}>▾</Text>
								</Pressable>
							</View>
							{visibleRecentRows.length > 0 ? (
								visibleRecentRows.map(({ key, ref, projectName }) => (
									<SessionRow
										key={key}
										nodeID={ref.nodeID}
										session={ref.session}
										nodeName={nodeNameByID[ref.nodeID]}
										nodeColor={nodeColorByID[ref.nodeID]}
										projectName={projectName}
										state={sessionState(ref.nodeID, ref.session.id)}
									/>
								))
							) : (
								<Text style={styles.recentEmpty}>该项目暂无最近会话</Text>
							)}
						</View>
					) : null}

					{!settingsLoading && !settings?.sessionToken ? (
						<Pressable style={styles.link} onPress={() => router.push("/login")}>
							<Text style={styles.linkText}>登录以查看电脑</Text>
						</Pressable>
					) : null}
				</ScrollView>
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
			<OptionSheet
				visible={recentFilterOpen}
				title="按项目筛选"
				items={recentProjectItems}
				onSelect={(key) => {
					setRecentFilterOpen(false);
					setRecentFilterKey(key === ALL_PROJECTS_OPTION_KEY ? null : key);
				}}
				onClose={() => setRecentFilterOpen(false)}
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
		center: { flex: 1 },
		centerContent: {
			flexGrow: 1,
			justifyContent: "center",
			paddingHorizontal: 20,
			paddingVertical: 32,
		},
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
		attachments: { flexDirection: "row", flexWrap: "wrap", gap: 6 },
		attachment: {
			flexDirection: "row",
			alignItems: "center",
			gap: 4,
			maxWidth: 220,
			borderRadius: 10,
			paddingLeft: 8,
			paddingRight: 2,
			paddingVertical: 2,
			backgroundColor: colors.surfaceMuted,
		},
		attachmentName: { flexShrink: 1, fontSize: 12, color: colors.textSecondary },
		attachmentRemove: { paddingHorizontal: 4, paddingVertical: 2 },
		attachmentRemoveText: { fontSize: 14, color: colors.textMuted },
		attach: {
			width: 34,
			height: 34,
			borderRadius: 17,
			alignItems: "center",
			justifyContent: "center",
			backgroundColor: colors.surfaceMuted,
		},
		attachText: { fontSize: 18, lineHeight: 20, color: colors.textSecondary },
		recent: { width: "100%", marginTop: 20, gap: 2 },
		recentHeader: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			paddingHorizontal: 4,
		},
		recentFilter: {
			flexDirection: "row",
			alignItems: "center",
			gap: 2,
			paddingVertical: 2,
			flexShrink: 1,
			maxWidth: 220,
		},
		recentFilterText: { flexShrink: 1, fontSize: 12, color: colors.accent },
		recentFilterCaret: { fontSize: 10, color: colors.accent },
		recentEmpty: {
			fontSize: 12,
			color: colors.textMuted,
			paddingHorizontal: 4,
			paddingVertical: 8,
		},
		recentTitle: {
			fontSize: 12,
			fontWeight: "600",
			color: colors.textMuted,
			paddingHorizontal: 4,
			paddingBottom: 2,
		},
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
