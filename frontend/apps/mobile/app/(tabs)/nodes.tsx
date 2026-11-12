import { StyleSheet, Text, View } from "react-native";

export default function NodesScreen() {
	return (
		<View style={styles.container}>
			<Text style={styles.title}>Nodes</Text>
			<Text style={styles.hint}>Registered nodes will appear here.</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	container: { flex: 1, padding: 16, gap: 8 },
	title: { fontSize: 20, fontWeight: "600" },
	hint: { color: "#666" },
});
