import { router, Stack } from "expo-router";
import * as Notifications from "expo-notifications";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { AppState } from "react-native";

import { useOverview } from "../src/overview/use-overview";
import { routeFromNotification } from "../src/push/route";
import { useSettings } from "../src/settings/use-settings";
import { AppMenu } from "../src/ui/app-menu";

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
	// 菜单里的待审批角标由总览快照汇总，避免为徽标新增接口。
	const { settings } = useSettings();
	const { nodes } = useOverview(settings);
	const pendingCount = nodes.reduce(
		(total, node) =>
			total + node.sessions.reduce((sum, session) => sum + session.pending_permissions, 0),
		0,
	);

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
		<>
			<StatusBar style="auto" />
			<Stack screenOptions={{ headerRight: () => <AppMenu pendingCount={pendingCount} /> }}>
				<Stack.Screen name="(tabs)" options={{ headerShown: false }} />
				<Stack.Screen name="settings" options={{ title: "Settings" }} />
				<Stack.Screen name="session/[node]/[id]" options={{ title: "Session" }} />
			</Stack>
		</>
	);
}
