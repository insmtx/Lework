import { useEffect, useMemo, useState } from "react";
import { Pressable, Text } from "react-native";

import { createClient } from "../../src/api/client";
import {
	ensureNotificationPermission,
	notifyCompletion,
	registerForPush,
	unregisterPush,
} from "../../src/push/register";
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
	const [testing, setTesting] = useState(false);
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

	// 试听只走本地通知，不依赖推送登记：能响说明播放与系统权限正常，
	// 不响则问题在通知权限或系统渠道，而不是任务触发。
	const testSound = async () => {
		setTesting(true);
		setStatus(null);
		setFailed(false);
		try {
			if (!(await ensureNotificationPermission())) {
				note("系统通知权限未开启，试听提醒被系统拦下。", true);
				return;
			}
			await notifyCompletion({
				nodeID: "",
				sessionID: "sound-test",
				kind: "done",
				title: "执行完成",
				body: "这是一条试听提醒",
			});
			note("已发送试听提醒；没听到声音请检查系统里的「执行结果」渠道。");
		} catch (error) {
			note(error instanceof Error ? error.message : "发送试听提醒失败", true);
		} finally {
			setTesting(false);
		}
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
				const registered = await registerForPush(settings, client);
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
					hint="关闭后仍会显示横幅，只是不响铃。"
					value={preferences.soundEnabled}
					onValueChange={(value) => {
						if (value) void ensureNotificationPermission();
						setPreference("soundEnabled", value);
					}}
				/>
				<Text style={styles.hint}>
					两类提醒各用一个系统通知渠道，可在系统设置里分别关闭声音。后台提醒依赖 App
					进程存活，建议把 lework 设为「不受电池限制」，否则熄屏久了可能被系统冻结。
				</Text>
				<Pressable
					accessibilityRole="button"
					disabled={testing}
					style={[styles.button, testing && styles.buttonDisabled]}
					onPress={() => void testSound()}
				>
					<Text style={styles.buttonText}>{testing ? "发送中…" : "试听完成提示音"}</Text>
				</Pressable>
			</SettingsSection>

			{status ? <Text style={[styles.status, failed && styles.statusFailed]}>{status}</Text> : null}
		</SettingsPage>
	);
}
