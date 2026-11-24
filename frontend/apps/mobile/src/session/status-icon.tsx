import { StyleSheet, Text } from "react-native";

import { useFontScale } from "../theme/use-theme";
import { type SessionStatus, statusGlyph, statusLabel } from "./status";

export interface SessionStatusIconProps {
	state: SessionStatus | null;
	/** 静态字形的主色；来自 statusColor，调用方负责按语气取色。 */
	color: string;
	/** 字形大小；运行中的色块按固定尺寸，不随字号放大。 */
	size?: number;
}

/**
 * SessionStatusIcon 用图形而不是文字表达静态状态。
 *
 * running/waiting 由整行扫光表达，这里返回 null，避免同一屏出现两套动效；
 * 其余状态用单字形成对比强烈的符号，完整含义只放在无障碍标签里。
 */
export function SessionStatusIcon({ state, color, size = 14 }: SessionStatusIconProps) {
	const fontScale = useFontScale();
	if (!state) return null;

	if (state === "running" || state === "waiting_permission") {
		return null;
	}

	// 状态图标不是主题样式表里的 Text，单独套用同一个倍率，避免标题变大而状态仍很小。
	const scaled = Math.round(size * fontScale);

	return (
		<Text
			accessible
			accessibilityLabel={statusLabel(state)}
			style={[styles.glyph, { color, fontSize: scaled, lineHeight: scaled + 2 }]}
		>
			{statusGlyph(state)}
		</Text>
	);
}

const styles = StyleSheet.create({
	glyph: { fontWeight: "700", textAlign: "center" },
});
