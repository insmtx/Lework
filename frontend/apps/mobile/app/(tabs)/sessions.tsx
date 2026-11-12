import { StyleSheet, Text, View } from "react-native";

export default function SessionsScreen() {
	return (
		<View style={styles.container}>
			<Text style={styles.title}>Sessions</Text>
			<Text style={styles.hint}>Sessions from connected nodes will appear here.</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	container: { flex: 1, padding: 16, gap: 8 },
	title: { fontSize: 20, fontWeight: "600" },
	hint: { color: "#666" },
});
