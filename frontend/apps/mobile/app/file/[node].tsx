import { router, useLocalSearchParams } from "expo-router";
import { useEffect, useState } from "react";
import { Image, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { createClient } from "../../src/api/client";
import { downloadFile, fileDownloadUrl, previewCachePath } from "../../src/files/download";
import {
	formatFileSize,
	mimeTypeFor,
	openWithSystemApp,
	previewKind,
	truncationNotice,
} from "../../src/files/viewer";
import { fileBaseDirectory } from "../../src/session/file-links";
import { MarkdownBody } from "../../src/session/markdown";
import type { Settings } from "../../src/settings/settings";
import type { Theme } from "../../src/theme/theme";
import { useTheme, useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";
import { LoadingScreen } from "../../src/ui/loading";

type Preview =
	| { kind: "markdown" | "text"; content: string; size: number; truncated: boolean }
	| { kind: "image" | "binary"; uri: string; size: number };

function param(value: string | string[] | undefined): string {
	return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

function fileName(path: string): string {
	const split = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
	return path.slice(split + 1) || path;
}

async function downloadToCache(
	settings: Settings,
	nodeID: string,
	directory: string,
	path: string,
	onProgress?: (percent: number) => void,
): Promise<string> {
	const { cacheDirectory } = await import("expo-file-system/legacy");
	if (!cacheDirectory) {
		throw new Error("当前设备没有可用的缓存目录");
	}
	return downloadFile({
		url: fileDownloadUrl(settings, nodeID, directory, path),
		headers: { Authorization: `Bearer ${settings.sessionToken}` },
		destination: previewCachePath(cacheDirectory, nodeID, path),
		onProgress,
	});
}

async function localFileSize(uri: string): Promise<number> {
	try {
		const { getInfoAsync } = await import("expo-file-system/legacy");
		const info = await getInfoAsync(uri);
		return info.exists && !info.isDirectory ? (info.size ?? 0) : 0;
	} catch {
		return 0;
	}
}

/**
 * 文件预览页：文本与 Markdown 由节点按块读出后应用内渲染，图片与 PDF 等
 * 二进制下载到缓存，后者交给系统查看器。只读，不写回节点。
 */
export default function FilePreviewScreen() {
	const params = useLocalSearchParams<{ node: string; directory?: string; path?: string }>();
	const nodeID = param(params.node);
	const directory = param(params.directory);
	const path = param(params.path);
	const { settings, settingsLoading } = useAppShell();
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	useScreenTitle(fileName(path) || "文件预览");

	const [preview, setPreview] = useState<Preview | null>(null);
	const [loading, setLoading] = useState(true);
	const [progress, setProgress] = useState<number | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	useEffect(() => {
		if (!settings) return;
		if (!nodeID || !path || !directory) {
			setError("文件路径不完整，请从会话里重新打开。");
			setLoading(false);
			return;
		}
		let cancelled = false;
		setLoading(true);
		setPreview(null);
		setError(null);
		setProgress(null);

		const load = async () => {
			const kind = previewKind(path);
			if (kind === "markdown" || kind === "text" || kind === "auto") {
				const result = await createClient(settings).readFile(nodeID, directory, path);
				if (cancelled) return;
				if (result.encoding === "utf-8") {
					setPreview({
						kind: kind === "markdown" ? "markdown" : "text",
						content: result.content,
						size: result.size,
						truncated: result.truncated,
					});
					setLoading(false);
					return;
				}
				// base64 说明节点判断为二进制，落到下面的下载分支。
			}
			const uri = await downloadToCache(settings, nodeID, directory, path, (percent) => {
				if (!cancelled) setProgress(percent);
			});
			if (cancelled) return;
			setPreview({
				kind: kind === "image" ? "image" : "binary",
				uri,
				size: await localFileSize(uri),
			});
			setLoading(false);
		};

		load().catch((caught: unknown) => {
			if (cancelled) return;
			setError(caught instanceof Error ? caught.message : "打开文件失败");
			setLoading(false);
		});

		return () => {
			cancelled = true;
		};
	}, [settings, nodeID, directory, path]);

	const openFullFile = async () => {
		if (!settings || !nodeID) return;
		setBusy(true);
		setError(null);
		try {
			const uri = await downloadToCache(settings, nodeID, directory, path, setProgress);
			await openWithSystemApp(uri, path);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "下载文件失败");
		} finally {
			setBusy(false);
			setProgress(null);
		}
	};

	if (settingsLoading) {
		return <LoadingScreen text="正在打开文件…" />;
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>未连接</Text>
				<Text style={styles.hint}>先在设置里填写控制面地址与令牌。</Text>
				<Pressable
					accessibilityRole="button"
					style={styles.button}
					onPress={() => router.push("/settings")}
				>
					<Text style={styles.buttonText}>打开设置</Text>
				</Pressable>
			</View>
		);
	}

	if (loading) {
		return <LoadingScreen text={progress === null ? "正在打开文件…" : `正在下载 ${progress}%`} />;
	}

	const shownSize = preview && "size" in preview ? preview.size : 0;
	// 只有文本/Markdown 预读会知道是否被截断；图片和二进制总是完整下载。
	const truncatedText =
		preview && (preview.kind === "markdown" || preview.kind === "text")
			? truncationNotice(preview.truncated)
			: null;

	return (
		<View style={styles.screen}>
			<View style={styles.header}>
				<Text style={styles.fileName} numberOfLines={2}>
					{fileName(path)}
				</Text>
				<Text selectable style={styles.path} numberOfLines={2}>
					{directory}
				</Text>
				{shownSize > 0 ? <Text style={styles.meta}>{formatFileSize(shownSize)}</Text> : null}
			</View>

			{error ? <Text style={styles.error}>{error}</Text> : null}

			{preview?.kind === "markdown" ? (
				<ScrollView contentContainerStyle={styles.content}>
					<MarkdownBody
						content={preview.content}
						scheme={theme.scheme}
						nodeID={nodeID}
						directory={fileBaseDirectory(directory, path)}
					/>
				</ScrollView>
			) : null}

			{preview?.kind === "text" ? (
				<ScrollView contentContainerStyle={styles.content}>
					<Text selectable style={styles.mono}>
						{preview.content}
					</Text>
				</ScrollView>
			) : null}

			{truncatedText ? (
				<View style={styles.truncatedBar}>
					<Text style={styles.truncatedText}>{truncatedText}</Text>
					<Pressable
						accessibilityRole="button"
						disabled={busy}
						style={[styles.secondaryButton, busy && styles.disabled]}
						onPress={() => void openFullFile()}
					>
						<Text style={styles.secondaryButtonText}>
							{busy && progress !== null ? `下载中 ${progress}%` : "打开完整文件"}
						</Text>
					</Pressable>
				</View>
			) : null}

			{preview?.kind === "image" ? (
				<ScrollView contentContainerStyle={styles.content}>
					<Image source={{ uri: preview.uri }} resizeMode="contain" style={styles.image} />
				</ScrollView>
			) : null}

			{preview?.kind === "binary" ? (
				<View style={styles.content}>
					<View style={styles.binaryCard}>
						<Text style={styles.binaryName}>{fileName(path)}</Text>
						<Text style={styles.meta}>
							{[formatFileSize(preview.size), mimeTypeFor(path) ?? "未知类型"]
								.filter((part) => part !== "" && part !== "0 B")
								.join(" · ")}
						</Text>
						<Pressable
							accessibilityRole="button"
							disabled={busy}
							style={[styles.button, busy && styles.disabled]}
							onPress={async () => {
								setBusy(true);
								setError(null);
								try {
									await openWithSystemApp(preview.uri, path);
								} catch (caught) {
									setError(caught instanceof Error ? caught.message : "无法打开文件");
								} finally {
									setBusy(false);
								}
							}}
						>
							<Text style={styles.buttonText}>用系统应用打开</Text>
						</Pressable>
					</View>
				</View>
			) : null}

			{!preview && error ? (
				<View style={styles.center}>
					<Pressable accessibilityRole="button" style={styles.button} onPress={() => router.back()}>
						<Text style={styles.buttonText}>返回</Text>
					</Pressable>
				</View>
			) : null}
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
			gap: 2,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		fileName: { fontSize: 16, fontWeight: "600", color: colors.text },
		path: { fontFamily: "monospace", fontSize: 11, color: colors.textMuted },
		meta: { fontSize: 12, color: colors.textSecondary },
		content: { padding: 16 },
		mono: { fontFamily: "monospace", fontSize: 12, lineHeight: 18, color: colors.text },
		image: { width: "100%", height: 360, borderRadius: 8 },
		error: { color: colors.danger, paddingHorizontal: 16, paddingTop: 8 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		button: {
			marginTop: 8,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
			alignItems: "center",
		},
		buttonText: { color: colors.actionText, fontWeight: "600" },
		secondaryButton: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 8,
			alignItems: "center",
		},
		secondaryButtonText: { color: colors.text, fontWeight: "600" },
		disabled: { opacity: 0.5 },
		truncatedBar: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 12,
			paddingHorizontal: 16,
			paddingVertical: 8,
			backgroundColor: colors.surfaceMuted,
			borderTopWidth: 1,
			borderTopColor: colors.borderSubtle,
		},
		truncatedText: { flex: 1, fontSize: 12, color: colors.textSecondary },
		binaryCard: {
			borderWidth: 1,
			borderColor: colors.borderSubtle,
			backgroundColor: colors.surface,
			borderRadius: 12,
			padding: 16,
			gap: 6,
		},
		binaryName: { fontFamily: "monospace", fontSize: 14, color: colors.text },
	});
}
