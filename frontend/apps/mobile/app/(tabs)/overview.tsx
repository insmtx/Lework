import { StyleSheet, Text, View } from "react-native";

export default function OverviewScreen() {
	return (
		<View style={styles.container}>
			<Text style={styles.title}>Overview</Text>
			<Text style={styles.hint}>All nodes and running sessions will appear here.</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	container: { flex: 1, padding: 16, gap: 8 },
	title: { fontSize: 20, fontWeight: "600" },
	hint: { color: "#666" },
});
