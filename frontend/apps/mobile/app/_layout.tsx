import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";

export default function RootLayout() {
	return (
		<>
			<StatusBar style="auto" />
			<Stack>
				<Stack.Screen name="(tabs)" options={{ headerShown: false }} />
				<Stack.Screen name="settings" options={{ title: "Settings" }} />
				<Stack.Screen name="session/[node]/[id]" options={{ title: "Session" }} />
			</Stack>
		</>
	);
}
