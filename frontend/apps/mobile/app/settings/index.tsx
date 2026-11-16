import { router } from "expo-router";

import { SettingsMenuCard, SettingsMenuRow, SettingsPage } from "../../src/settings/components";
import {
	describeAccount,
	describeAppearance,
	describeProcess,
	describeReminder,
	describeServer,
} from "../../src/settings/summary";
import { useThemePreference } from "../../src/theme/theme-context";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";
import { describeVersion } from "../../src/update/format";
import { useUpdate } from "../../src/update/update-context";

/**
 * 设置首页只做目录：每行一个主题，右侧带当前值。
 *
 * 具体选项放在二级页，首页因此始终只有四行；用户不必在一条长页面里
 * 混合「改外观」和「填服务器地址」两种完全不同的操作。
 */
export default function SettingsIndexScreen() {
	useScreenTitle("设置");
	const { settings } = useAppShell();
	const { preferences } = useThemePreference();
	const { state, currentVersion, manualCheck } = useUpdate();
	const connected = settings !== null;

	return (
		<SettingsPage>
			<SettingsMenuCard>
				<SettingsMenuRow
					label="账号"
					value={describeAccount(settings?.email)}
					onPress={() => router.push(settings?.sessionToken ? "/settings/account" : "/login")}
				/>
				<SettingsMenuRow
					label="界面"
					value={describeAppearance(preferences)}
					onPress={() => router.push("/settings/appearance")}
				/>
				<SettingsMenuRow
					label="提醒"
					value={describeReminder(preferences, settings?.notifications ?? false, connected)}
					onPress={() => router.push("/settings/notifications")}
				/>
				<SettingsMenuRow
					label="服务器设置"
					value={describeServer(settings?.baseUrl)}
					onPress={() => router.push("/settings/server")}
				/>
				<SettingsMenuRow
					label="过程展示"
					value={describeProcess(preferences)}
					onPress={() => router.push("/settings/process")}
				/>
				{/* 版本行不跳页：点一下就是一次手动检查，右侧文案跟随更新状态机。 */}
				<SettingsMenuRow
					label="版本"
					value={describeVersion(currentVersion, state)}
					onPress={manualCheck}
					last
				/>
			</SettingsMenuCard>
		</SettingsPage>
	);
}
