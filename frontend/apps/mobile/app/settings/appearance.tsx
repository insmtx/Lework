import { Segmented, SettingsPage, SettingsSection } from "../../src/settings/components";
import type { FontSize } from "../../src/settings/preferences";
import type { ThemeMode } from "../../src/theme/theme";
import { useThemePreference } from "../../src/theme/theme-context";
import { useScreenTitle } from "../../src/ui/app-shell-context";

const THEME_OPTIONS: { value: ThemeMode; label: string }[] = [
	{ value: "system", label: "跟随系统" },
	{ value: "light", label: "亮色" },
	{ value: "dark", label: "暗色" },
];

const FONT_OPTIONS: { value: FontSize; label: string }[] = [
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

			<SettingsSection
				title="界面字号"
				hint="整体缩放界面文字：标题、状态、思考过程、工具输出与正文一起变化。"
			>
				<Segmented
					options={FONT_OPTIONS}
					value={preferences.fontSize}
					onChange={(value) => setPreference("fontSize", value)}
				/>
			</SettingsSection>
		</SettingsPage>
	);
}
