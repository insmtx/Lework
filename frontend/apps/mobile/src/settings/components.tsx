import type { ReactNode } from "react";
import { Pressable, ScrollView, StyleSheet, Switch, Text, View } from "react-native";

import type { Theme } from "../theme/theme";
import { useTheme, useThemedStyles } from "../theme/use-theme";

/** Option 是分段控件的一项；泛型让调用方的取值保持字面量类型。 */
export interface Option<T extends string> {
	value: T;
	label: string;
}

/**
 * SettingsPage 是设置类页面的统一滚动容器。
 *
 * 所有设置页共用同一套内边距与间距，二级页之间切换时不会出现内容跳动。
 */
export function SettingsPage({ children }: { children: ReactNode }) {
	const styles = useThemedStyles(createSettingsStyles);
	return (
		<ScrollView contentContainerStyle={styles.container} keyboardShouldPersistTaps="handled">
			{children}
		</ScrollView>
	);
}

/** SettingsSection 是「标题 + 说明 + 卡片」的一组设置。 */
export function SettingsSection({
	title,
	hint,
	children,
}: {
	title: string;
	hint?: string;
	children: ReactNode;
}) {
	const styles = useThemedStyles(createSettingsStyles);
	return (
		<View style={styles.section}>
			<Text style={styles.sectionTitle}>{title}</Text>
			{hint ? <Text style={styles.sectionHint}>{hint}</Text> : null}
			<View style={styles.card}>{children}</View>
		</View>
	);
}

/** SettingsMenuCard 是菜单列表容器，行之间由 MenuRow 自己画分隔线。 */
export function SettingsMenuCard({ children }: { children: ReactNode }) {
	const styles = useThemedStyles(createSettingsStyles);
	return <View style={styles.menuCard}>{children}</View>;
}

/**
 * SettingsMenuRow 是设置首页的一行：左边名称，右边当前值。
 *
 * 当前值让用户不进二级页也能确认现状，例如「跟随系统 · 标准字号」。
 */
export function SettingsMenuRow({
	label,
	value,
	onPress,
	last,
}: {
	label: string;
	value: string;
	onPress: () => void;
	last?: boolean;
}) {
	const styles = useThemedStyles(createSettingsStyles);
	return (
		<Pressable
			accessibilityRole="button"
			accessibilityLabel={`${label}，${value}`}
			style={[styles.menuRow, !last && styles.menuRowDivider]}
			onPress={onPress}
		>
			<Text style={styles.menuLabel}>{label}</Text>
			<View style={styles.menuValueWrap}>
				<Text style={styles.menuValue} numberOfLines={1}>
					{value}
				</Text>
				<Text style={styles.menuChevron}>›</Text>
			</View>
		</Pressable>
	);
}

/** Segmented 是互斥选项控件；选项少且都需要可见时比下拉菜单更快。 */
export function Segmented<T extends string>({
	options,
	value,
	onChange,
}: {
	options: Option<T>[];
	value: T;
	onChange: (value: T) => void;
}) {
	const styles = useThemedStyles(createSettingsStyles);
	return (
		<View style={styles.segmented}>
			{options.map((option) => {
				const active = option.value === value;
				return (
					<Pressable
						key={option.value}
						accessibilityRole="button"
						accessibilityState={{ selected: active }}
						style={[styles.segment, active && styles.segmentActive]}
						onPress={() => onChange(option.value)}
					>
						<Text style={[styles.segmentText, active && styles.segmentTextActive]}>
							{option.label}
						</Text>
					</Pressable>
				);
			})}
		</View>
	);
}

/** ToggleRow 是「名称 + 说明 + 开关」的一行，用于本地偏好或设备登记开关。 */
export function ToggleRow({
	label,
	hint,
	value,
	onValueChange,
	disabled,
}: {
	label: string;
	hint: string;
	value: boolean;
	onValueChange: (value: boolean) => void;
	disabled?: boolean;
}) {
	const theme = useTheme();
	const styles = useThemedStyles(createSettingsStyles);
	return (
		<View style={[styles.toggleRow, disabled && styles.toggleDisabled]}>
			<View style={styles.toggleText}>
				<Text style={styles.label}>{label}</Text>
				<Text style={styles.hint}>{hint}</Text>
			</View>
			<Switch
				value={value}
				onValueChange={onValueChange}
				disabled={disabled}
				trackColor={{ false: theme.colors.borderStrong, true: theme.colors.accent }}
				thumbColor={theme.colors.surface}
			/>
		</View>
	);
}

/**
 * createSettingsStyles 是所有设置页共享的样式表。
 *
 * 定义在模块顶层，useThemedStyles 才能按主题缓存；设置页较多，重复的间距
 * 规则集中在这里，避免各页自己写一套。
 */
export function createSettingsStyles(theme: Theme) {
	const { colors, radii } = theme;
	return StyleSheet.create({
		container: { padding: 16, gap: 20, paddingBottom: 40 },
		section: { gap: 6 },
		sectionTitle: { fontSize: 12, fontWeight: "700", color: colors.textSecondary },
		sectionHint: { fontSize: 12, color: colors.textMuted },
		card: {
			backgroundColor: colors.surface,
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: radii.lg,
			padding: 14,
			gap: 10,
		},
		menuCard: {
			backgroundColor: colors.surface,
			borderWidth: 1,
			borderColor: colors.border,
			borderRadius: radii.lg,
			overflow: "hidden",
		},
		menuRow: {
			flexDirection: "row",
			alignItems: "center",
			gap: 12,
			minHeight: 52,
			paddingHorizontal: 14,
			paddingVertical: 12,
		},
		menuRowDivider: { borderBottomWidth: 1, borderBottomColor: colors.borderSubtle },
		menuLabel: { fontSize: 15, color: colors.text },
		menuValueWrap: {
			marginLeft: "auto",
			flexDirection: "row",
			alignItems: "center",
			gap: 4,
			flexShrink: 1,
			maxWidth: "64%",
		},
		menuValue: { fontSize: 13, color: colors.textMuted, flexShrink: 1 },
		menuChevron: { fontSize: 16, color: colors.textMuted },
		label: { fontSize: 14, fontWeight: "600", color: colors.text },
		fieldLabel: { fontSize: 12, fontWeight: "600", color: colors.textSecondary, marginTop: 2 },
		hint: { fontSize: 12, color: colors.textMuted },
		input: {
			borderWidth: 1,
			borderColor: colors.inputBorder,
			backgroundColor: colors.inputBackground,
			color: colors.text,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 10,
			fontSize: 16,
		},
		toggleRow: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 12,
		},
		toggleDisabled: { opacity: 0.45 },
		toggleText: { flex: 1, gap: 2 },
		segmented: { flexDirection: "row", gap: 8 },
		segment: {
			flex: 1,
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingVertical: 8,
			alignItems: "center",
		},
		segmentActive: { backgroundColor: colors.action, borderColor: colors.action },
		segmentText: { fontSize: 13, color: colors.textStrong, fontWeight: "600" },
		segmentTextActive: { color: colors.actionText },
		statusRow: { flexDirection: "row", alignItems: "center", gap: 6 },
		statusDot: { width: 7, height: 7, borderRadius: 4 },
		statusText: { flex: 1, fontSize: 12, color: colors.textSecondary },
		button: {
			marginTop: 4,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingVertical: 12,
			alignItems: "center",
		},
		buttonDisabled: { opacity: 0.6 },
		buttonText: { color: colors.actionText, fontSize: 16, fontWeight: "600" },
		status: { fontSize: 12, color: colors.success },
		statusFailed: { color: colors.danger },
		pairButton: {
			alignSelf: "flex-start",
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 6,
		},
		pairButtonText: { fontSize: 13, color: colors.text, fontWeight: "600" },
	});
}
