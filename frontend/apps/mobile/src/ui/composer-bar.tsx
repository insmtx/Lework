import type { ReactNode } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";

export interface ComposerChipProps {
	/** 前缀，如「电脑」「项目」；技能、快捷命令复用同一个组件。 */
	label: string;
	value: string;
	/** 状态点；技能与命令没有在线概念，传 none。 */
	status?: "online" | "offline" | "none";
	/** 可选节点色；用同色描边与浅底区分当前选中的是哪台电脑。 */
	accentColor?: string;
	disabled?: boolean;
	onPress?: () => void;
}

/**
 * ComposerChip 是输入框上方的轻量选择入口。
 *
 * 只负责展示与触发：选中值、可选范围都由调用方的纯函数决定，避免把
 * 目标解析逻辑复制进 UI。
 */
export function ComposerChip(props: ComposerChipProps) {
	const styles = useThemedStyles(createChipStyles);
	const status = props.status ?? "none";
	const interactive = Boolean(props.onPress) && !props.disabled;

	return (
		<Pressable
			disabled={!interactive}
			onPress={props.onPress}
			style={[
				styles.chip,
				props.disabled && styles.disabled,
				// 8 位十六进制让色值自带透明度，省掉一套 alpha 换算。
				props.accentColor && {
					borderColor: props.accentColor,
					backgroundColor: `${props.accentColor}14`,
				},
			]}
		>
			{status === "none" ? null : (
				<View style={[styles.dot, status === "online" ? styles.dotOnline : styles.dotOffline]} />
			)}
			<Text style={styles.label}>{props.label}</Text>
			<Text style={styles.value} numberOfLines={1}>
				{props.value}
			</Text>
			{interactive ? <Text style={styles.caret}>▾</Text> : null}
		</Pressable>
	);
}

/**
 * ComposerBar 承载输入框上方的一行 chip。
 *
 * 横向滚动而不是换行或收进「更多」菜单：技能、快捷命令以后会继续往右排，
 * 布局不需要跟着改。
 */
export function ComposerBar(props: { children: ReactNode }) {
	const styles = useThemedStyles(createBarStyles);
	return (
		<ScrollView
			horizontal
			showsHorizontalScrollIndicator={false}
			keyboardShouldPersistTaps="handled"
			contentContainerStyle={styles.bar}
		>
			{props.children}
		</ScrollView>
	);
}

function createChipStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		chip: {
			flexDirection: "row",
			alignItems: "center",
			gap: 4,
			maxWidth: 220,
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: 14,
			backgroundColor: colors.surface,
			paddingHorizontal: 10,
			paddingVertical: 5,
		},
		disabled: { opacity: 0.45 },
		dot: { width: 6, height: 6, borderRadius: 3 },
		dotOnline: { backgroundColor: colors.success },
		dotOffline: { backgroundColor: colors.offline },
		label: { fontSize: 11, color: colors.textMuted },
		value: { flexShrink: 1, fontSize: 12, color: colors.text, fontWeight: "600" },
		caret: { fontSize: 10, color: colors.textMuted },
	});
}

function createBarStyles(_theme: Theme) {
	return StyleSheet.create({
		bar: { flexDirection: "row", alignItems: "center", gap: 8, paddingRight: 4 },
	});
}
