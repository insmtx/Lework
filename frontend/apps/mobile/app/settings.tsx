import { StyleSheet, Text, View } from "react-native";

// 设置页在 Task 4 实现；先占位，保证 Settings 入口可导航。
export default function SettingsScreen() {
	return (
		<View style={styles.container}>
			<Text style={styles.title}>Settings</Text>
			<Text style={styles.hint}>Server address and token will be configured here.</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	container: { flex: 1, padding: 16, gap: 8 },
	title: { fontSize: 20, fontWeight: "600" },
	hint: { color: "#666" },
});
