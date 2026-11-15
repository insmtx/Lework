import { Segmented, SettingsPage, SettingsSection } from "../../src/settings/components";
import type { ChatFontSize } from "../../src/settings/preferences";
import type { ThemeMode } from "../../src/theme/theme";
import { useThemePreference } from "../../src/theme/theme-context";
import { useScreenTitle } from "../../src/ui/app-shell-context";

const THEME_OPTIONS: { value: ThemeMode; label: string }[] = [
	{ value: "system", label: "跟随系统" },
	{ value: "light", label: "亮色" },
	{ value: "dark", label: "暗色" },
];

const FONT_OPTIONS: { value: ChatFontSize; label: string }[] = [
	{ value: "small", label: "小" },
	{ value: "standard", label: "标准" },
	{ value: "large", label: "大" },
];

/** 界面设置：外观与字号都是纯本地偏好，切换后立即生效。 */
export default function AppearanceScreen() {
	useScreenTitle("界面");
	const { preferences, setPreference, setMode } = useThemePreference();

	return (
		<SettingsPage>
			<SettingsSection title="外观" hint="跟随系统时随手机深浅色切换。">
				<Segmented options={THEME_OPTIONS} value={preferences.themeMode} onChange={setMode} />
			</SettingsSection>

			<SettingsSection title="会话字号" hint="只影响消息正文，导航和按钮保持不变。">
				<Segmented
					options={FONT_OPTIONS}
					value={preferences.chatFontSize}
					onChange={(value) => setPreference("chatFontSize", value)}
				/>
			</SettingsSection>
		</SettingsPage>
	);
}
