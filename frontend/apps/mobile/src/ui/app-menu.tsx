import { router } from "expo-router";
import { useState } from "react";
import { Modal, Pressable, StyleSheet, Text, View } from "react-native";

import { MENU_ITEMS } from "./menu-items";

/**
 * AppMenu 是右上角菜单入口。
 *
 * 节点与设置刻意不进主导航：项目是唯一一级对象，节点只是项目上的标签。
 * pendingCount > 0 时在入口上显示圆点，提醒有审批要处理。
 */
export function AppMenu({ pendingCount }: { pendingCount: number }) {
	const [open, setOpen] = useState(false);

	const close = () => setOpen(false);

	return (
		<>
			<Pressable
				accessibilityLabel="menu"
				hitSlop={12}
				style={styles.trigger}
				onPress={() => setOpen(true)}
			>
				<Text style={styles.triggerText}>⋯</Text>
				{pendingCount > 0 ? <View style={styles.dot} /> : null}
			</Pressable>

			<Modal animationType="fade" transparent visible={open} onRequestClose={close}>
				<Pressable style={styles.backdrop} onPress={close}>
					<View style={styles.panel}>
						{MENU_ITEMS.map((item) => (
							<Pressable
								key={item.key}
								style={styles.item}
								onPress={() => {
									close();
									router.push(item.href as never);
								}}
							>
								<Text style={styles.itemLabel}>{item.label}</Text>
								{item.badge === "pending" && pendingCount > 0 ? (
									<View style={styles.badge}>
										<Text style={styles.badgeText}>{pendingCount}</Text>
									</View>
								) : null}
							</Pressable>
						))}
					</View>
				</Pressable>
			</Modal>
		</>
	);
}

const styles = StyleSheet.create({
	trigger: { paddingHorizontal: 16, paddingVertical: 8 },
	triggerText: { fontSize: 22, fontWeight: "600" },
	dot: {
		position: "absolute",
		top: 6,
		right: 12,
		width: 8,
		height: 8,
		borderRadius: 4,
		backgroundColor: "#b45309",
	},
	backdrop: { flex: 1, backgroundColor: "rgba(15, 23, 42, 0.25)" },
	panel: {
		position: "absolute",
		top: 52,
		right: 12,
		minWidth: 160,
		backgroundColor: "#fff",
		borderRadius: 12,
		paddingVertical: 6,
		shadowColor: "#000",
		shadowOpacity: 0.15,
		shadowRadius: 12,
		elevation: 4,
	},
	item: {
		flexDirection: "row",
		alignItems: "center",
		justifyContent: "space-between",
		paddingHorizontal: 16,
		paddingVertical: 12,
		gap: 12,
	},
	itemLabel: { fontSize: 16 },
	badge: {
		minWidth: 20,
		paddingHorizontal: 6,
		paddingVertical: 2,
		borderRadius: 10,
		backgroundColor: "#b45309",
		alignItems: "center",
	},
	badgeText: { color: "#fff", fontSize: 12, fontWeight: "600" },
});
