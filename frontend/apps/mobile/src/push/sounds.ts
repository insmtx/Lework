/**
 * 推送提示音的规范名。
 *
 * 同一个文件名被三处引用：`assets/sounds/` 下的音频、`app.json` 里
 * expo-notifications 插件的 `sounds` 数组、以及 Android 渠道配置。改名时
 * 必须三处一起改，否则渠道会静默回落到系统默认音。
 */
export const APPROVAL_SOUND = "approval.wav";
export const COMPLETION_SOUND = "done.wav";

export const PUSH_SOUND_FILES = [APPROVAL_SOUND, COMPLETION_SOUND] as const;
