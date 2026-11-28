import type { FileInfo } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	FlatList,
	Pressable,
	RefreshControl,
	ScrollView,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { createClient } from "../../src/api/client";
import { fileRoute } from "../../src/config";
import { baseName, isSafeRelative, joinPath } from "../../src/files/paths";
import { pickBrowseNode, shouldLoadDir, sortEntries } from "../../src/files/tree";
import { projectDirectoryOnNode, projectDisplayName } from "../../src/projects/model";
import type { Theme } from "../../src/theme/theme";
import { useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";
import { LoadingScreen } from "../../src/ui/loading";
import { OptionSheet } from "../../src/ui/option-sheet";

/**
 * 项目文件页：以项目在当前节点上的检出目录为根，逐层浏览只读文件。
 *
 * 一次只显示一层，点目录进入下一层，点文件交给现有预览页；目录结果按层
 * 缓存，切换节点或刷新时整体丢弃，避免把上一个节点的内容留在屏幕上。
 */
export default function ProjectFilesScreen() {
	const params = useLocalSearchParams<{ key?: string | string[] }>();
	const projectKey = Array.isArray(params.key) ? (params.key[0] ?? "") : (params.key ?? "");
	const { settings, settingsLoading, projects: state } = useAppShell();
	const styles = useThemedStyles(createStyles);

	const project = state.projects.find((item) => item.key === projectKey) ?? null;
	const projectName = project ? projectDisplayName(project) : "文件";
	useScreenTitle(projectName);

	const onlineNodeIDs = useMemo(
		() => state.nodes.filter((node) => node.connected).map((node) => node.id),
		[state.nodes],
	);
	const nodeNameByID = useMemo(() => {
		const out: Record<string, string> = {};
		for (const node of state.nodes) out[node.id] = node.name;
		return out;
	}, [state.nodes]);
	const nodeLabel = (nodeID: string) => nodeNameByID[nodeID] || nodeID;

	const [pickedNodeID, setPickedNodeID] = useState<string | null>(null);
	const nodeID = project ? pickBrowseNode(project, pickedNodeID, onlineNodeIDs) : null;
	const canonical = project && nodeID ? projectDirectoryOnNode(project, nodeID) : "";

	const [path, setPath] = useState("");
	const [cache, setCache] = useState<Record<string, FileInfo[]>>({});
	const [pending, setPending] = useState<Record<string, true>>({});
	const [error, setError] = useState<string | null>(null);
	const [pickerOpen, setPickerOpen] = useState(false);
	// 节点切换或刷新后，旧请求的结果必须丢弃，不能盖到新列表上。
	const generation = useRef(0);

	const loadDir = useCallback(
		async (dir: string) => {
			if (!settings || !nodeID || !canonical) return;
			const request = generation.current;
			setPending((prev) => ({ ...prev, [dir]: true }));
			try {
				const entries = await createClient(settings).listFiles(nodeID, canonical, dir);
				if (request !== generation.current) return;
				setCache((prev) => ({ ...prev, [dir]: entries }));
				setError(null);
			} catch (caught) {
				if (request !== generation.current) return;
				setError(caught instanceof Error ? caught.message : "无法读取目录");
			} finally {
				if (request === generation.current) {
					setPending((prev) => {
						const next = { ...prev };
						delete next[dir];
						return next;
					});
				}
			}
		},
		[canonical, nodeID, settings],
	);

	useEffect(() => {
		if (!projectKey || !nodeID || !canonical) return;
		generation.current += 1;
		setPath("");
		setCache({});
		setPending({});
		setError(null);
		void loadDir("");
	}, [projectKey, nodeID, canonical, loadDir]);

	const refresh = useCallback(() => {
		if (!nodeID || !canonical) return;
		generation.current += 1;
		setCache({});
		setPending({});
		setError(null);
		void loadDir(path);
	}, [canonical, loadDir, nodeID, path]);

	const goTo = useCallback(
		(next: string) => {
			setError(null);
			setPath(next);
			if (shouldLoadDir(cache, next)) void loadDir(next);
		},
		[cache, loadDir],
	);

	const enterDirectory = useCallback(
		(entry: FileInfo) => {
			if (!isSafeRelative(entry.path)) return;
			goTo(joinPath(path, entry.path));
		},
		[goTo, path],
	);

	const openFile = useCallback(
		(entry: FileInfo) => {
			if (!nodeID || !canonical || !isSafeRelative(entry.path)) return;
			router.push(fileRoute(nodeID, canonical, joinPath(path, entry.path)));
		},
		[canonical, nodeID, path],
	);

	const entries = useMemo(() => sortEntries(cache[path] ?? []), [cache, path]);
	const loading = pending[path] === true;
	const segments = path.split("/").filter(Boolean);
	const crumbs = segments.map((segment, index) => ({
		segment,
		path: segments.slice(0, index + 1).join("/"),
	}));

	if (settingsLoading) {
		return <LoadingScreen text="正在加载项目…" />;
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>未连接</Text>
				<Text style={styles.hint}>先在设置里填写控制面地址与令牌。</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>打开设置</Text>
				</Pressable>
			</View>
		);
	}

	if (state.loading && !project) {
		return <LoadingScreen text="正在加载项目…" />;
	}

	if (!project) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>项目不存在</Text>
				<Text style={styles.hint}>它可能已从所有节点上消失。</Text>
				<Pressable style={styles.button} onPress={() => router.back()}>
					<Text style={styles.buttonText}>返回</Text>
				</Pressable>
			</View>
		);
	}

	if (!nodeID || !canonical) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>节点离线</Text>
				<Text style={styles.hint}>没有在线节点，暂时无法浏览文件。</Text>
				<Pressable style={styles.button} onPress={() => router.back()}>
					<Text style={styles.buttonText}>返回</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<View style={styles.screen}>
			<View style={styles.header}>
				<ScrollView
					horizontal
					showsHorizontalScrollIndicator={false}
					contentContainerStyle={styles.crumbs}
				>
					<Pressable onPress={() => goTo("")}>
						<Text style={styles.crumb}>{projectName}</Text>
					</Pressable>
					{crumbs.map((crumb) => (
						<View key={crumb.path} style={styles.crumbGroup}>
							<Text style={styles.crumbSep}>›</Text>
							<Pressable onPress={() => goTo(crumb.path)}>
								<Text style={styles.crumb} numberOfLines={1}>
									{crumb.segment}
								</Text>
							</Pressable>
						</View>
					))}
				</ScrollView>
				<View style={styles.toolbar}>
					{project.nodes.length > 1 ? (
						<Pressable style={styles.nodeChip} onPress={() => setPickerOpen(true)}>
							<Text style={styles.nodeChipText}>{nodeLabel(nodeID)} ▾</Text>
						</Pressable>
					) : (
						<Text style={styles.nodeChipText}>{nodeLabel(nodeID)}</Text>
					)}
					<Pressable hitSlop={8} onPress={refresh}>
						<Text style={styles.refresh}>刷新</Text>
					</Pressable>
				</View>
				{error && entries.length > 0 ? <Text style={styles.error}>{error}</Text> : null}
			</View>

			<FlatList
				style={styles.list}
				contentContainerStyle={styles.listContent}
				data={entries}
				keyExtractor={(entry) => entry.path}
				refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
				renderItem={({ item }) => (
					<Pressable
						style={styles.row}
						onPress={() => (item.type === "directory" ? enterDirectory(item) : openFile(item))}
					>
						<Text style={styles.rowLabel} numberOfLines={1}>
							{baseName(item.path) || item.path}
						</Text>
						{item.type === "directory" ? <Text style={styles.chevron}>›</Text> : null}
					</Pressable>
				)}
				ListEmptyComponent={
					<View style={styles.empty}>
						{error ? (
							<>
								<Text style={styles.error}>{error}</Text>
								<Pressable style={styles.button} onPress={refresh}>
									<Text style={styles.buttonText}>重试</Text>
								</Pressable>
							</>
						) : loading ? (
							<Text style={styles.hint}>正在加载…</Text>
						) : (
							<Text style={styles.hint}>此目录为空</Text>
						)}
					</View>
				}
			/>

			<OptionSheet
				visible={pickerOpen}
				title="选择节点"
				items={project.nodes.map((node) => ({
					key: node.node_id,
					label: nodeLabel(node.node_id),
					status: node.online ? "在线" : "离线",
					disabled: !node.online,
					selected: node.node_id === nodeID,
				}))}
				onSelect={(picked) => {
					setPickerOpen(false);
					setPickedNodeID(picked);
				}}
				onClose={() => setPickerOpen(false)}
			/>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		screen: { flex: 1, backgroundColor: colors.background },
		header: {
			paddingHorizontal: 16,
			paddingTop: 12,
			paddingBottom: 8,
			gap: 6,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		crumbs: { alignItems: "center", gap: 2 },
		crumbGroup: { flexDirection: "row", alignItems: "center", gap: 2, maxWidth: 180 },
		crumb: { fontSize: 13, color: colors.accent },
		crumbSep: { fontSize: 13, color: colors.textMuted },
		toolbar: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
		},
		nodeChip: { paddingVertical: 2 },
		nodeChipText: { fontSize: 12, color: colors.textSecondary },
		refresh: { fontSize: 12, color: colors.accent },
		list: { flex: 1 },
		listContent: { padding: 8, flexGrow: 1 },
		row: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 8,
			paddingHorizontal: 8,
			paddingVertical: 12,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		rowLabel: { flex: 1, fontSize: 15, color: colors.text },
		chevron: { fontSize: 16, color: colors.textMuted },
		empty: { paddingVertical: 32, alignItems: "center", gap: 8 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 20, fontWeight: "700", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { fontSize: 12, color: colors.danger },
		button: {
			marginTop: 8,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
		},
		buttonText: { color: colors.actionText, fontWeight: "600" },
	});
}
