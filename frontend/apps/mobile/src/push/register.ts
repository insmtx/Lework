import type { ApiClient } from "@lework/api";
import Constants from "expo-constants";
import * as Device from "expo-device";
import * as Notifications from "expo-notifications";
import { Platform } from "react-native";

import type { Settings } from "../settings/settings";

// ANDROID_CHANNEL_ID 必须与服务端推送里的 ChannelID 一致，否则 Android 8+
// 会把通知落到默认渠道，静音或没有横幅提示。
export const ANDROID_CHANNEL_ID = "permissions";

async function ensureAndroidChannel(): Promise<void> {
	if (Platform.OS !== "android") return;
	await Notifications.setNotificationChannelAsync(ANDROID_CHANNEL_ID, {
		name: "审批提醒",
		description: "节点请求审批时提醒",
		importance: Notifications.AndroidImportance.HIGH,
		lockscreenVisibility: Notifications.AndroidNotificationVisibility.PUBLIC,
		sound: "default",
		enableVibrate: true,
	});
}

// registerForPush 申请通知权限并登记 Expo Push Token。
//
// 模拟器没有推送能力，projectId 缺失时拿不到 token；两种情况都静默返回
// null，让设置页保持“已连接但未登记推送”，而不是把连接判为失败。
// token 只提交给控制面，不进日志、不进通知负载。
export async function registerForPush(
	settings: Settings,
	client: ApiClient,
): Promise<string | null> {
	void settings;
	if (!Device.isDevice) return null;

	await ensureAndroidChannel();

	let permission = await Notifications.getPermissionsAsync();
	if (permission.status !== "granted") {
		permission = await Notifications.requestPermissionsAsync();
	}
	if (permission.status !== "granted") return null;

	const projectId = Constants.expoConfig?.extra?.eas?.projectId;
	if (typeof projectId !== "string" || projectId === "") {
		console.warn("push notifications need extra.eas.projectId in app.json");
		return null;
	}

	const token = (await Notifications.getExpoPushTokenAsync({ projectId })).data;
	const device = await client.registerDevice({
		name: Device.modelName ?? "Android",
		platform: "android",
		push_token: token,
	});
	return device.id;
}

// unregisterPush 在关闭通知时撤销登记，避免控制面继续向这台设备推送。
export async function unregisterPush(client: ApiClient, deviceID: string): Promise<void> {
	await client.deleteDevice(deviceID);
}
