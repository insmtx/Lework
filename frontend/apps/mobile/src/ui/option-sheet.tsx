import { Modal, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";

export interface OptionSheetItem {
	key: string;
	label: string;
	/** 第二行说明，通常是路径或原因。 */
	detail?: string;
	/** 右侧状态文案，如「在线」「离线」。 */
	status?: string;
	/** 左侧色块；用于节点颜色等需要可视化取值的选项。 */
	swatch?: string;
	tone?: "normal" | "muted" | "danger";
	selected?: boolean;
	disabled?: boolean;
}

export interface OptionSheetProps {
	visible: boolean;
	title: string;
	subtitle?: string;
	items: OptionSheetItem[];
	emptyText?: string;
	onSelect: (key: string) => void;
	onClose: () => void;
}

/**
 * OptionSheet 是 chip 的通用底部弹层。
 *
 * 电脑、项目共用一套；以后技能与快捷命令只提供 items，不需要再复制一套
 * 弹层。选中态、禁用态、空态与底部安全区都在这里，页面只负责数据。
 */
export function OptionSheet(props: OptionSheetProps) {
	const insets = useSafeAreaInsets();
	const styles = useThemedStyles(createStyles);

	return (
		<Modal transparent visible={props.visible} animationType="fade" onRequestClose={props.onClose}>
			<View style={styles.backdrop}>
				<Pressable style={StyleSheet.absoluteFill} onPress={props.onClose} />
				<View style={[styles.sheet, { paddingBottom: Math.max(insets.bottom, 16) }]}>
					<View style={styles.header}>
						<Text style={styles.title}>{props.title}</Text>
						{props.subtitle ? <Text style={styles.subtitle}>{props.subtitle}</Text> : null}
					</View>
					{props.items.length === 0 ? (
						<Text style={styles.empty}>{props.emptyText ?? "暂无选项"}</Text>
					) : (
						<ScrollView style={styles.list} keyboardShouldPersistTaps="handled">
							{props.items.map((item) => (
								<Pressable
									key={item.key}
									disabled={item.disabled}
									onPress={() => props.onSelect(item.key)}
									style={[styles.item, item.disabled && styles.itemDisabled]}
								>
									{item.swatch ? (
										<View style={[styles.swatch, { backgroundColor: item.swatch }]} />
									) : null}
									<View style={styles.itemMain}>
										<Text
											numberOfLines={1}
											style={[
												styles.label,
												item.tone === "muted" && styles.muted,
												item.tone === "danger" && styles.danger,
												item.selected && styles.selectedLabel,
											]}
										>
											{item.label}
										</Text>
										{item.detail ? (
											<Text numberOfLines={1} style={styles.detail}>
												{item.detail}
											</Text>
										) : null}
									</View>
									{item.status ? <Text style={styles.status}>{item.status}</Text> : null}
									{item.selected ? <Text style={styles.check}>✓</Text> : null}
								</Pressable>
							))}
						</ScrollView>
					)}
				</View>
			</View>
		</Modal>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		backdrop: { flex: 1, backgroundColor: colors.modalBackdrop, justifyContent: "flex-end" },
		sheet: {
			maxHeight: "70%",
			backgroundColor: colors.surface,
			borderTopLeftRadius: 16,
			borderTopRightRadius: 16,
			paddingTop: 16,
			paddingHorizontal: 16,
		},
		header: { gap: 4, marginBottom: 8 },
		title: { fontSize: 16, fontWeight: "600", color: colors.text },
		subtitle: { fontSize: 12, color: colors.textSecondary },
		empty: { paddingVertical: 24, fontSize: 13, color: colors.textSecondary, textAlign: "center" },
		list: { marginTop: 4 },
		item: {
			flexDirection: "row",
			alignItems: "center",
			gap: 10,
			paddingVertical: 12,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		itemDisabled: { opacity: 0.45 },
		swatch: { width: 14, height: 14, borderRadius: 7, borderWidth: 1, borderColor: colors.border },
		itemMain: { flex: 1, gap: 2 },
		label: { fontSize: 15, color: colors.text },
		selectedLabel: { fontWeight: "600" },
		muted: { color: colors.textSecondary },
		danger: { color: colors.danger },
		detail: { fontSize: 12, color: colors.textSecondary },
		status: { fontSize: 12, color: colors.textMuted },
		check: { fontSize: 14, color: colors.accent },
	});
}
