import { useLocalSearchParams } from "expo-router";
import { StyleSheet, Text, View } from "react-native";

// 会话页在 Task 8 实现；先占位，保证深链与列表跳转可用。
export default function SessionScreen() {
	const { node, id } = useLocalSearchParams<{ node: string; id: string }>();
	return (
		<View style={styles.container}>
			<Text style={styles.title}>Session</Text>
			<Text style={styles.hint}>
				{node} / {id}
			</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	container: { flex: 1, padding: 16, gap: 8 },
	title: { fontSize: 20, fontWeight: "600" },
	hint: { color: "#666" },
});
