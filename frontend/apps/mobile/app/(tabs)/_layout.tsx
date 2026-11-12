import { Link, Tabs } from "expo-router";
import { Pressable, Text } from "react-native";

export default function TabsLayout() {
	return (
		<Tabs
			screenOptions={{
				headerRight: () => (
					<Link href="/settings" asChild>
						<Pressable hitSlop={12} style={{ paddingHorizontal: 16 }}>
							<Text style={{ fontSize: 16 }}>Settings</Text>
						</Pressable>
					</Link>
				),
			}}
		>
			<Tabs.Screen name="overview" options={{ title: "Overview" }} />
			<Tabs.Screen name="nodes" options={{ title: "Nodes" }} />
			<Tabs.Screen name="sessions" options={{ title: "Sessions" }} />
		</Tabs>
	);
}
