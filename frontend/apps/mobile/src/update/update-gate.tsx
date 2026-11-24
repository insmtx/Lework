// UpdateGate 是全局更新 Modal：挂在根布局 AppShell 内，任何页面都会盖住提示。
// installing 不显示（系统安装器已在前台），可见性判断统一走状态机的
// isUpdateVisible，这里只负责按阶段渲染文案与按钮。
import { Modal, Pressable, StyleSheet, Text, View } from "react-native";

import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";
import { isUpdateVisible, type UpdateState } from "./state";
import { useUpdate } from "./update-context";

type GateStyles = ReturnType<typeof createUpdateGateStyles>;

interface GateActions {
	onDismiss: () => void;
	onCancelDownload: () => void;
	onInstall: () => void;
	onRetry: () => void;
	onStart: () => void;
	onSkip: () => void;
}

// 版本号可能带 v 前缀（LATEST.txt 允许 v?x.y.z），统一剥掉后再补，
// 避免出现「发现新版本 vv0.9.0」。
function displayVersion(version: string): string {
	return version.replace(/^v/i, "");
}

export function UpdateGate() {
	const { state, install, dismiss, startUpdate, skipVersion, cancelDownload, retry } = useUpdate();
	const styles = useThemedStyles(createUpdateGateStyles);

	if (!isUpdateVisible(state)) return null;

	return (
		<Modal visible transparent animationType="fade" statusBarTranslucent onRequestClose={dismiss}>
			<View style={styles.backdrop}>
				<View style={styles.card}>
					{renderGateBody(state, styles, {
						onDismiss: dismiss,
						onCancelDownload: cancelDownload,
						onInstall: install,
						onRetry: retry,
						onStart: startUpdate,
						onSkip: skipVersion,
					})}
				</View>
			</View>
		</Modal>
	);
}

function renderGateBody(state: UpdateState, styles: GateStyles, actions: GateActions) {
	switch (state.phase) {
		case "available":
			// 发现新版本只提问，不自动下载；用户在「立即更新」与「跳过此版本」中选。
			return (
				<>
					<Text style={styles.title}>发现新版本 v{displayVersion(state.version)}</Text>
					<Text style={styles.detail}>是否现在更新？跳过后该版本不再提示，换新版本会再提醒。</Text>
					<View style={styles.actions}>
						<GateButton label="跳过此版本" onPress={actions.onSkip} styles={styles} />
						<GateButton label="立即更新" onPress={actions.onStart} styles={styles} primary />
					</View>
				</>
			);
		case "downloading":
			return (
				<>
					<Text style={styles.title}>发现新版本 v{displayVersion(state.version)}</Text>
					<View style={styles.progressWrap}>
						<View style={styles.progressTrack}>
							<View style={[styles.progressFill, { width: `${state.progress}%` }]} />
						</View>
						<Text style={styles.detail}>正在下载 {state.progress}%</Text>
					</View>
					<View style={styles.actions}>
						{/* 取消只中止本次下载，不记跳过：下次常规检查或手动检查会重新询问。 */}
						<GateButton label="取消下载" onPress={actions.onCancelDownload} styles={styles} />
					</View>
				</>
			);
		case "downloaded":
			return (
				<>
					<Text style={styles.title}>更新已下载</Text>
					<Text style={styles.detail}>
						v{displayVersion(state.version)} 安装包已就绪，点「立即安装」完成升级。
					</Text>
					<View style={styles.actions}>
						<GateButton label="稍后" onPress={actions.onDismiss} styles={styles} />
						<GateButton label="立即安装" onPress={actions.onInstall} styles={styles} primary />
					</View>
				</>
			);
		case "error":
			return (
				<>
					<Text style={styles.title}>更新失败</Text>
					<Text style={styles.detail} numberOfLines={4}>
						{state.message}
					</Text>
					<View style={styles.actions}>
						<GateButton label="稍后" onPress={actions.onDismiss} styles={styles} />
						<GateButton label="重试" onPress={actions.onRetry} styles={styles} primary />
					</View>
				</>
			);
		default:
			// idle / checking / installing 由 isUpdateVisible 挡在门外；兜底防御
			// 阶段扩展后遗漏渲染。
			return null;
	}
}

function GateButton({
	label,
	onPress,
	styles,
	primary,
}: {
	label: string;
	onPress: () => void;
	styles: GateStyles;
	primary?: boolean;
}) {
	return (
		<Pressable
			accessibilityRole="button"
			style={[styles.button, primary ? styles.buttonPrimary : styles.buttonSecondary]}
			onPress={onPress}
		>
			<Text style={[styles.buttonText, primary && styles.buttonTextPrimary]}>{label}</Text>
		</Pressable>
	);
}

// 样式工厂放在模块顶层（引用稳定），useThemedStyles 才能按主题缓存并统一
// 缩放字号；配色与设置页组件保持同一语义色板。
function createUpdateGateStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		backdrop: {
			flex: 1,
			justifyContent: "center",
			paddingHorizontal: 28,
			backgroundColor: colors.backdrop,
		},
		card: {
			backgroundColor: colors.surface,
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: 16,
			padding: 20,
			gap: 10,
		},
		title: { fontSize: 17, fontWeight: "700", color: colors.text },
		detail: { fontSize: 14, color: colors.textSecondary },
		progressWrap: { gap: 6 },
		progressTrack: {
			height: 8,
			borderRadius: 4,
			backgroundColor: colors.surfaceSubtle,
			overflow: "hidden",
		},
		progressFill: { height: "100%", borderRadius: 4, backgroundColor: colors.accent },
		actions: { flexDirection: "row", justifyContent: "flex-end", gap: 10, marginTop: 6 },
		button: {
			borderRadius: 10,
			paddingHorizontal: 16,
			paddingVertical: 10,
			minWidth: 88,
			alignItems: "center",
		},
		buttonPrimary: { backgroundColor: colors.action },
		buttonSecondary: { borderWidth: 1, borderColor: colors.borderStrong },
		buttonText: { fontSize: 14, fontWeight: "600", color: colors.text },
		buttonTextPrimary: { color: colors.actionText },
	});
}
