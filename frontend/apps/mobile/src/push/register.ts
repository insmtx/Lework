import type { ApiClient } from "@lework/api";
import Constants from "expo-constants";
import * as Device from "expo-device";
import * as Notifications from "expo-notifications";
import { Platform } from "react-native";

import type { Settings } from "../settings/settings";
import {
	ANDROID_CHANNEL_ID,
	ANDROID_CHANNEL_ID_COMPLETIONS,
	androidChannels,
	type ChannelImportance,
	LEGACY_CHANNEL_IDS,
} from "./channels";
import type { CompletionAlert } from "./completion-alert";

export { ANDROID_CHANNEL_ID, ANDROID_CHANNEL_ID_COMPLETIONS };

// 渠道定义里的语义重要性映射到原生枚举；其余字段（名称、声音）直接来自
// channels.ts，保证测试断言的配置就是真正注册到系统里的那份。
const IMPORTANCE: Record<ChannelImportance, Notifications.AndroidImportance> = {
	high: Notifications.AndroidImportance.HIGH,
	default: Notifications.AndroidImportance.DEFAULT,
};

// 前台是否响铃由「提醒」里的声音开关决定。模块级变量而不是 context：
// setNotificationHandler 在模块加载时注册，拿不到 React 状态。
let foregroundSoundEnabled = true;

/** setForegroundSoundEnabled 让前台通知处理器跟随用户的声音偏好。 */
export function setForegroundSoundEnabled(enabled: boolean): void {
	foregroundSoundEnabled = enabled;
}

/** isForegroundSoundEnabled 返回当前前台通知是否应响铃。 */
export function isForegroundSoundEnabled(): boolean {
	return foregroundSoundEnabled;
}

/**
 * ensureAndroidChannels 注册两类提醒的渠道，并清掉旧版 ID。
 *
 * 声音在渠道创建时定下后归系统管理，应用无法再改，所以这里只保证渠道
 * 存在；用户想静音时在系统设置里单独关掉对应渠道即可。旧 ID 必须删除，
 * 否则设置里会残留一条永不触发的「审批提醒 / 执行结果」。
 */
export async function ensureAndroidChannels(): Promise<void> {
	if (Platform.OS !== "android") return;
	for (const id of LEGACY_CHANNEL_IDS) {
		await Notifications.deleteNotificationChannelAsync(id);
	}
	for (const channel of androidChannels()) {
		await Notifications.setNotificationChannelAsync(channel.id, {
			name: channel.name,
			description: channel.description,
			importance: IMPORTANCE[channel.importance],
			lockscreenVisibility: Notifications.AndroidNotificationVisibility.PUBLIC,
			sound: channel.sound,
			enableVibrate: channel.enableVibrate,
			vibrationPattern: channel.vibrationPattern,
		});
	}
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

	await ensureAndroidChannels();

	if (!(await ensureNotificationPermission())) return null;

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

/**
 * ensureNotificationPermission 补齐通知权限；渠道已由启动流程创建。
 *
 * 返回 false 表示用户拒绝或系统不支持，调用方应静默降级，不要重复弹窗。
 */
export async function ensureNotificationPermission(): Promise<boolean> {
	if (!Device.isDevice) return false;
	let permission = await Notifications.getPermissionsAsync();
	if (permission.status !== "granted") {
		permission = await Notifications.requestPermissionsAsync();
	}
	return permission.status === "granted";
}

/**
 * notifyCompletion 立即发一条本地完成提醒。
 *
 * 走 completions-v4 渠道，声音与震动交给系统渠道决定；没有权限时安静放弃，
 * 不能用一次任务完成把用户拽去授权弹窗。
 */
export async function notifyCompletion(alert: CompletionAlert): Promise<void> {
	const permission = await Notifications.getPermissionsAsync();
	if (permission.status !== "granted") return;

	// 渠道可能在系统设置里被删掉，或被更早的版本以错误配置创建；每次发之前
	// 补建一次。建渠道失败不能连提醒一起丢掉，系统会回落到默认渠道。
	try {
		await ensureAndroidChannels();
	} catch {
		// 忽略：发通知本身仍然有意义。
	}

	await Notifications.scheduleNotificationAsync({
		// 与会话 ID 同名：服务端推送的 tag 也是会话 ID，Android 会把两条通知
		// 折叠成一条，推送真正可达时不会重复弹两次。
		identifier: alert.sessionID,
		content: {
			title: alert.title,
			body: alert.body,
			sound: "default",
			data: { node: alert.nodeID, session: alert.sessionID, kind: alert.kind },
		},
		trigger: { channelId: ANDROID_CHANNEL_ID_COMPLETIONS },
	});
}

/**
 * completionChannelState 返回「执行结果」渠道在系统里的声音状态。
 *
 * 试听结果只有「响/不响」两种可观察输出，其余原因都在系统设置里；把渠道
 * 状态直接写回提示文案，用户不必再靠猜。
 */
export async function completionChannelState(): Promise<string> {
	if (Platform.OS !== "android") return "仅 Android 使用通知渠道";
	const channel = await Notifications.getNotificationChannelAsync(ANDROID_CHANNEL_ID_COMPLETIONS);
	if (!channel) return "「执行结果」渠道不存在";
	if (channel.importance === Notifications.AndroidImportance.NONE)
		return "「执行结果」渠道已被关闭";
	if (!channel.sound) return "「执行结果」渠道的声音已被关闭";
	return "「执行结果」渠道正常";
}
