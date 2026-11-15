import * as Notifications from "expo-notifications";
import { router, Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { AppState } from "react-native";

import { routeFromNotification } from "../src/push/route";
import { ThemeProvider } from "../src/theme/theme-context";
import { useTheme } from "../src/theme/use-theme";
import { AppShell } from "../src/ui/app-shell";
import { DrawerButton } from "../src/ui/top-bar";

// 前台收到通知时也显示横幅：审批请求可能就在用户盯着 App 的时候到达。
Notifications.setNotificationHandler({
	handleNotification: async () => ({
		shouldShowBanner: true,
		shouldShowList: true,
		shouldPlaySound: false,
		shouldSetBadge: false,
	}),
});

function openFromNotification(data: unknown) {
	const route = routeFromNotification(data);
	if (route) router.push(route);
}

export default function RootLayout() {
	// ThemeProvider 在导航器外层：设置页改主题时，Stack 头部与所有页面一起重绘。
	return (
		<ThemeProvider>
			<RootNavigator />
		</ThemeProvider>
	);
}

function RootNavigator() {
	const theme = useTheme();

	useEffect(() => {
		const subscription = Notifications.addNotificationResponseReceivedListener((response) => {
			openFromNotification(response.notification.request.content.data);
		});

		// 冷启动时监听器还没挂上，单独取一次上次点击的通知；处理完清掉，
		// 避免下次启动又跳一遍。
		void Notifications.getLastNotificationResponseAsync().then(async (response) => {
			if (!response) return;
			openFromNotification(response.notification.request.content.data);
			await Notifications.clearLastNotificationResponseAsync();
		});

		// 打开 App 即视为已读：不能让锁屏上的旧审批一直挂着。
		void Notifications.dismissAllNotificationsAsync();

		return () => subscription.remove();
	}, []);

	useEffect(() => {
		const subscription = AppState.addEventListener("change", (state) => {
			if (state === "active") void Notifications.dismissAllNotificationsAsync();
		});
		return () => subscription.remove();
	}, []);

	return (
		<AppShell>
			<StatusBar style={theme.scheme === "dark" ? "light" : "dark"} />
			<Stack
				screenOptions={{
					headerRight: () => <DrawerButton />,
					headerStyle: { backgroundColor: theme.colors.surface },
					headerTintColor: theme.colors.text,
					headerTitleStyle: { color: theme.colors.text },
					contentStyle: { backgroundColor: theme.colors.background },
				}}
			>
				<Stack.Screen name="index" options={{ headerShown: false }} />
				<Stack.Screen name="projects" options={{ title: "项目" }} />
				<Stack.Screen name="projects/[key]" options={{ title: "项目" }} />
				<Stack.Screen name="pending" options={{ title: "待审批" }} />
				<Stack.Screen name="nodes" options={{ title: "节点" }} />
				<Stack.Screen name="settings" options={{ title: "设置" }} />
				<Stack.Screen name="connect" options={{ headerShown: false }} />
				<Stack.Screen name="session/[node]/[id]" options={{ headerShown: false }} />
			</Stack>
		</AppShell>
	);
}
