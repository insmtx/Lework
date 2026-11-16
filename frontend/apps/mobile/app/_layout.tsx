import * as Notifications from "expo-notifications";
import { router, Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { AppState } from "react-native";

import { isForegroundSoundEnabled } from "../src/push/register";
import { routeFromNotification } from "../src/push/route";
import { ThemeProvider, useThemePreference } from "../src/theme/theme-context";
import { useFontScale, useTheme } from "../src/theme/use-theme";
import { AppShell } from "../src/ui/app-shell";
import { LoadingScreen } from "../src/ui/loading";
import { DrawerButton } from "../src/ui/top-bar";
import { UpdateProvider } from "../src/update/update-context";
import { UpdateGate } from "../src/update/update-gate";

// 前台收到通知时也显示横幅：审批请求可能就在用户盯着 App 的时候到达。
// 是否响铃跟随设置里的「声音提醒」，所以这里读模块级开关而不是写死 false。
Notifications.setNotificationHandler({
	handleNotification: async () => ({
		shouldShowBanner: true,
		shouldShowList: true,
		shouldPlaySound: isForegroundSoundEnabled(),
		shouldSetBadge: false,
	}),
});

function openFromNotification(data: unknown) {
	const route = routeFromNotification(data);
	if (route) router.push(route);
}

export default function RootLayout() {
	// ThemeProvider 在导航器外层：设置页改主题时，Stack 头部与所有页面一起重绘。
	// UpdateProvider 在主题之内：更新 Modal 走主题样式；两者都与导航同生命周期。
	return (
		<ThemeProvider>
			<UpdateProvider>
				<RootNavigator />
			</UpdateProvider>
		</ThemeProvider>
	);
}

function RootNavigator() {
	const theme = useTheme();
	const fontScale = useFontScale();
	const { ready } = useThemePreference();

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
			<StatusBar style={theme.scheme === "dark" ? "light" : "dark"} />
			{ready ? (
				<AppShell>
					<Stack
						screenOptions={{
							headerRight: () => <DrawerButton />,
							headerStyle: { backgroundColor: theme.colors.surface },
							headerTintColor: theme.colors.text,
							// 原生导航头不走主题样式表，单独套用同一个字号倍率。
							headerTitleStyle: {
								color: theme.colors.text,
								fontSize: Math.round(17 * fontScale),
							},
							contentStyle: { backgroundColor: theme.colors.background },
						}}
					>
						<Stack.Screen name="index" options={{ headerShown: false }} />
						<Stack.Screen name="projects" options={{ title: "项目" }} />
						<Stack.Screen name="projects/[key]" options={{ title: "项目" }} />
						<Stack.Screen name="pending" options={{ title: "待审批" }} />
						<Stack.Screen name="nodes" options={{ title: "我的电脑" }} />
						<Stack.Screen name="nodes/add" options={{ title: "添加电脑" }} />
						<Stack.Screen name="node/pair" options={{ headerShown: false }} />
						<Stack.Screen name="settings/index" options={{ title: "设置" }} />
						<Stack.Screen name="settings/appearance" options={{ title: "界面" }} />
						<Stack.Screen name="settings/notifications" options={{ title: "提醒" }} />
						<Stack.Screen name="settings/server" options={{ title: "服务器设置" }} />
						<Stack.Screen name="settings/process" options={{ title: "过程展示" }} />
						<Stack.Screen name="login" options={{ title: "登录", headerShown: false }} />
						<Stack.Screen name="settings/account" options={{ title: "账号" }} />
						<Stack.Screen name="session/[node]/[id]" options={{ headerShown: false }} />
					</Stack>
					{/* 更新 Modal 放在导航之外：任何页面都会覆盖，路由切换不打断下载提示。 */}
					<UpdateGate />
				</AppShell>
			) : (
				<LoadingScreen text="正在启动…" />
			)}
		</>
	);
}
