import { useEffect, useMemo, useState } from "react";
import { Text } from "react-native";

import { createClient } from "../../src/api/client";
import { registerForPush, unregisterPush } from "../../src/push/register";
import {
	createSettingsStyles,
	SettingsPage,
	SettingsSection,
	ToggleRow,
} from "../../src/settings/components";
import { createSecureSettingsStore } from "../../src/settings/secure-store";
import { useSettings } from "../../src/settings/use-settings";
import { useThemePreference } from "../../src/theme/theme-context";
import { useThemedStyles } from "../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";

/**
 * 提醒设置分两层：推送开关对应控制面上的设备登记，声音只是本地偏好。
 *
 * 没有连接控制面时推送开关不可用——此时登记必然失败，让用户先连接比
 * 弹一条错误更有用。
 */
export default function NotificationsScreen() {
	useScreenTitle("提醒");
	const styles = useThemedStyles(createSettingsStyles);
	const { settings, loading, reload } = useSettings();
	const { reloadSettings } = useAppShell();
	const { preferences, setPreference } = useThemePreference();
	const store = useMemo(() => createSecureSettingsStore(), []);

	const [notifications, setNotifications] = useState(settings?.notifications ?? false);
	const [busy, setBusy] = useState(false);
	const [status, setStatus] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);

	// 从服务器设置页返回时连接可能已经变化，这里以最新读到的值为准。
	useEffect(() => {
		setNotifications(settings?.notifications ?? false);
	}, [settings]);

	const note = (message: string, isError = false) => {
		setFailed(isError);
		setStatus(message);
	};

	const toggleNotifications = async (enabled: boolean) => {
		if (!settings?.sessionToken) {
			note("先登录账号，再开启推送。", true);
			return;
		}
		setBusy(true);
		setStatus(null);
		setFailed(false);
		try {
			const client = createClient(settings);
			let nextDeviceID = settings.deviceID;
			if (enabled) {
				const registered = await registerForPush(settings, client, preferences.soundEnabled);
				if (!registered) {
					throw new Error("推送未登记：请授予通知权限并配置 EAS project id。");
				}
				nextDeviceID = registered;
			} else if (settings.deviceID) {
				await unregisterPush(client, settings.deviceID);
				nextDeviceID = undefined;
			}

			const next = { ...settings, notifications: enabled, deviceID: nextDeviceID };
			await store.save(next);
			setNotifications(enabled);
			reload();
			reloadSettings();
			note(enabled ? "推送已开启。" : "推送已关闭。");
		} catch (error) {
			// 登记失败时开关回到真实状态，避免界面显示「已开启」却收不到通知。
			setNotifications(settings.notifications);
			note(error instanceof Error ? error.message : "切换推送失败", true);
		} finally {
			setBusy(false);
		}
	};

	return (
		<SettingsPage>
			<SettingsSection
				title="推送"
				hint={
					settings?.sessionToken
						? "审批请求到达时提醒，关闭后不再推送。"
						: "先登录账号，推送才能登记。"
				}
			>
				<ToggleRow
					label="推送通知"
					hint="会话需要审批时提醒。"
					value={notifications}
					onValueChange={(value) => void toggleNotifications(value)}
					disabled={busy || loading || !settings?.sessionToken}
				/>
			</SettingsSection>

			<SettingsSection title="声音" hint="只影响这台手机，不会改变控制面的推送内容。">
				<ToggleRow
					label="声音提醒"
					hint="审批通知响铃并震动；关闭后静默提醒。"
					value={preferences.soundEnabled}
					onValueChange={(value) => setPreference("soundEnabled", value)}
					disabled={!notifications}
				/>
				<Text style={styles.hint}>
					iOS 后台推送的提示音由服务端负载决定；这里控制的是前台收到通知时是否响铃。
				</Text>
			</SettingsSection>

			{status ? <Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text> : null}
		</SettingsPage>
	);
}
