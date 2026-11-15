import { ActivityIndicator, StyleSheet, Text, View } from "react-native";

import { useFontScale } from "../theme/use-theme";
import { type SessionStatus, statusGlyph, statusLabel } from "./status";

export interface SessionStatusIconProps {
	state: SessionStatus | null;
	/** 图标主色；来自 statusColor，调用方负责按语气取色。 */
	color: string;
	/** 字形大小；运行中的转圈按这个尺寸等比缩放。 */
	size?: number;
}

/**
 * SessionStatusIcon 用图形而不是文字表达会话状态。
 *
 * running 是持续动画的转圈，用户扫一眼就知道还在跑；其余状态用单字形成对
 * 比强烈的符号，避免同一行里出现大段文案。完整含义只放在无障碍标签里。
 */
export function SessionStatusIcon({ state, color, size = 14 }: SessionStatusIconProps) {
	const fontScale = useFontScale();
	if (!state) return null;

	// 状态图标不是主题样式表里的 Text，单独套用同一个倍率，避免标题变大而状态仍很小。
	const scaled = Math.round(size * fontScale);

	if (state === "running") {
		return (
			<View
				accessible
				accessibilityLabel={statusLabel(state)}
				style={[styles.spinner, { width: scaled, height: scaled }]}
			>
				<ActivityIndicator
					size="small"
					color={color}
					style={{ transform: [{ scale: 0.7 * fontScale }] }}
				/>
			</View>
		);
	}

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
	spinner: { alignItems: "center", justifyContent: "center" },
	glyph: { fontWeight: "700", textAlign: "center" },
});
