import { APPROVAL_SOUND, COMPLETION_SOUND } from "./sounds";

// ANDROID_CHANNEL_ID 必须与服务端推送里的 ChannelID 一致，否则 Android 8+
// 会把通知落到默认渠道，静音或没有横幅提示。
//
// ID 带版本后缀：Android 会「复活」同 ID 的已删除渠道并保留旧设置，因此
// 改声音只能换新 ID，不能删除后重建。
export const ANDROID_CHANNEL_ID = "permissions-v3";

// 执行结果走独立渠道：审批需要打断用户（HIGH + 急促震动），完成/失败也要
// 一条立刻能看见的横幅。两件事混在一个渠道里，用户没法只关掉其中一个。
export const ANDROID_CHANNEL_ID_COMPLETIONS = "completions-v4";

/** LEGACY_CHANNEL_IDS 是旧版本渠道，启动时清掉，避免系统设置里出现重复条目。 */
export const LEGACY_CHANNEL_IDS = [
	"permissions",
	"completions",
	"permissions-v2",
	"completions-v2",
	"completions-v3",
] as const;

/**
 * ChannelImportance 用语义值描述渠道的打断程度。
 *
 * 不用 expo-notifications 的原生枚举：这里的渠道定义要能在纯 Node 测试里
 * 断言，真正映射到 AndroidImportance 的动作留给 register.ts。
 */
export type ChannelImportance = "high" | "default";

export interface AndroidChannel {
	id: string;
	name: string;
	description: string;
	importance: ChannelImportance;
	sound: string;
	enableVibrate: boolean;
	/** vibrationPattern 按 Android 约定排列：[静默, 震动, 静默, 震动, …] 毫秒。 */
	vibrationPattern: number[];
}

/**
 * androidChannels 返回两类提醒的渠道定义。
 *
 * 提示音由渠道决定而不是推送负载：Android 8+ 创建渠道后声音就归系统管，
 * 所以两类事件从一开始就必须落在不同渠道、不同音效上。
 */
export function androidChannels(): AndroidChannel[] {
	return [
		{
			id: ANDROID_CHANNEL_ID,
			name: "审批提醒",
			description: "节点请求审批时播放审批提示音",
			importance: "high",
			sound: APPROVAL_SOUND,
			enableVibrate: true,
			// 两段急促震动：声音被静音时也能感到有人在等审批。
			vibrationPattern: [0, 250, 150, 250],
		},
		{
			id: ANDROID_CHANNEL_ID_COMPLETIONS,
			name: "执行结果",
			// DEFAULT 只会进通知栏，前台不弹横幅；用户盯着屏幕时等于没有提示。
			description: "会话执行完成或失败时播放完成提示音",
			importance: "high",
			sound: COMPLETION_SOUND,
			enableVibrate: true,
			// 单段轻震：知道跑完了，但不至于像审批那样催人。
			vibrationPattern: [0, 150],
		},
	];
}
