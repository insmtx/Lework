import { Text } from "react-native";

import {
	createSettingsStyles,
	Segmented,
	SettingsPage,
	SettingsSection,
} from "../../src/settings/components";
import type { ProcessDisplay, ToolDisplay } from "../../src/settings/preferences";
import { useThemePreference } from "../../src/theme/theme-context";
import { useThemedStyles } from "../../src/theme/use-theme";
import { useScreenTitle } from "../../src/ui/app-shell-context";

const PROCESS_OPTIONS: { value: ProcessDisplay; label: string }[] = [
	{ value: "hidden", label: "隐藏" },
	{ value: "brief", label: "摘要" },
	{ value: "full", label: "完整" },
];

const TOOL_OPTIONS: { value: ToolDisplay; label: string }[] = [
	{ value: "hidden", label: "隐藏" },
	{ value: "collapsed", label: "折叠" },
	{ value: "expanded", label: "展开" },
];

/**
 * 过程展示控制会话里显示多少执行细节。
 *
 * 它只影响呈现，不改变模型行为：隐藏思考不会让模型少思考，只是不占屏幕。
 */
export default function ProcessScreen() {
	useScreenTitle("过程展示");
	const styles = useThemedStyles(createSettingsStyles);
	const { preferences, setPreference } = useThemePreference();

	return (
		<SettingsPage>
			<SettingsSection title="思考过程" hint="隐藏完全不显示；摘要先给一段，点开看完整。">
				<Segmented
					options={PROCESS_OPTIONS}
					value={preferences.processDisplay}
					onChange={(value) => setPreference("processDisplay", value)}
				/>
			</SettingsSection>

			<SettingsSection title="工具调用" hint="折叠时只露工具名与状态，点一下展开输入输出。">
				<Segmented
					options={TOOL_OPTIONS}
					value={preferences.toolDisplay}
					onChange={(value) => setPreference("toolDisplay", value)}
				/>
				<Text style={styles.hint}>隐藏后工具卡片完全不出现，只保留回复正文。</Text>
			</SettingsSection>
		</SettingsPage>
	);
}
