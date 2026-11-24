import { StyleSheet, Text } from "react-native";

import { useFontScale } from "../theme/use-theme";
import { SessionStatusMark } from "../ui/session-status-mark";
import { type SessionStatus, statusGlyph, statusLabel } from "./status";

export interface SessionStatusIconProps {
	state: SessionStatus | null;
	/** 静态字形的主色；来自 statusColor，调用方负责按语气取色。 */
	color: string;
	/** running 色块使用的节点色；未设色时回退主题 accent。 */
	nodeColor?: string | null;
	/** 字形大小；运行中的色块按固定尺寸，不随字号放大。 */
	size?: number;
}

/**
 * SessionStatusIcon 用图形而不是文字表达会话状态。
 *
 * running/waiting 复用列表里的色块动画：扫一眼就知道还在跑还是在等人批准；
 * 其余状态用单字形成对比强烈的符号，避免同一行里出现大段文案。完整含义只放
 * 在无障碍标签里。
 */
export function SessionStatusIcon({ state, color, nodeColor, size = 14 }: SessionStatusIconProps) {
	const fontScale = useFontScale();
	if (!state) return null;

	if (state === "running" || state === "waiting_permission") {
		return <SessionStatusMark state={state} color={state === "running" ? nodeColor : undefined} />;
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
