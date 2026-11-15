import { router } from "expo-router";
import type { ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";
import { useAppShell } from "./app-shell-context";

/** DrawerButton 是页面左上角的菜单入口，任何页面都能打开主导航。 */
export function DrawerButton({ accessibilityLabel = "打开菜单" }: { accessibilityLabel?: string }) {
	const { openDrawer } = useAppShell();
	const styles = useThemedStyles(createStyles);

	return (
		<Pressable
			accessibilityLabel={accessibilityLabel}
			hitSlop={12}
			style={styles.iconButton}
			onPress={openDrawer}
		>
			<Text style={styles.menuText}>☰</Text>
		</Pressable>
	);
}

/**
 * BellButton 汇总所有节点的待审批数量并跳到待审批页。
 *
 * 数字来自总览快照，不额外请求；为 0 时不显示角标，免得顶栏一直是红的。
 */
export function BellButton({ onPress }: { onPress?: () => void }) {
	const { pendingCount } = useAppShell();
	const styles = useThemedStyles(createStyles);

	return (
		<Pressable
			accessibilityLabel="待审批"
			hitSlop={12}
			style={styles.iconButton}
			onPress={onPress ?? (() => router.push("/pending"))}
		>
			<Text style={styles.bellText}>🔔</Text>
			{pendingCount > 0 ? (
				<View style={styles.badge}>
					<Text style={styles.badgeText}>{pendingCount > 99 ? "99+" : pendingCount}</Text>
				</View>
			) : null}
		</Pressable>
	);
}

export interface ScreenTopBarProps {
	title: string;
	/** 会话页传切换器；其它页面不传，标题只读。 */
	onTitlePress?: () => void;
	/** 标题右侧的额外操作，例如会话页的「停止」。 */
	extra?: ReactNode;
}

/** ScreenTopBar 是首页之外的统一顶栏：菜单 + 名称 + 通知。 */
export function ScreenTopBar({ title, onTitlePress, extra }: ScreenTopBarProps) {
	const insets = useSafeAreaInsets();
	const styles = useThemedStyles(createStyles);

	return (
		<View style={[styles.bar, { paddingTop: insets.top + 4 }]}>
			<DrawerButton />
			<Pressable
				disabled={!onTitlePress}
				onPress={onTitlePress}
				style={styles.titleWrap}
				accessibilityLabel={onTitlePress ? "切换会话" : undefined}
			>
				<Text style={styles.title} numberOfLines={1}>
					{title}
				</Text>
				{onTitlePress ? <Text style={styles.chevron}>⌄</Text> : null}
			</Pressable>
			<View style={styles.actions}>
				{extra}
				<BellButton />
			</View>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		bar: {
			flexDirection: "row",
			alignItems: "center",
			gap: 4,
			paddingHorizontal: 8,
			paddingBottom: 8,
			backgroundColor: colors.surface,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		iconButton: { width: 36, height: 36, alignItems: "center", justifyContent: "center" },
		menuText: { fontSize: 20, color: colors.text },
		bellText: { fontSize: 17 },
		badge: {
			position: "absolute",
			top: 2,
			right: 0,
			minWidth: 16,
			height: 16,
			paddingHorizontal: 4,
			borderRadius: 8,
			backgroundColor: colors.badge,
			alignItems: "center",
			justifyContent: "center",
		},
		badgeText: { fontSize: 10, color: colors.badgeText, fontWeight: "700" },
		titleWrap: {
			flex: 1,
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "center",
			gap: 2,
		},
		title: { fontSize: 16, fontWeight: "600", color: colors.text, maxWidth: "88%" },
		chevron: { fontSize: 14, color: colors.textSecondary },
		actions: { flexDirection: "row", alignItems: "center", gap: 2 },
	});
}
