import type { SessionState } from "@lework/api";
import { StyleSheet, View } from "react-native";

import { statusLabel } from "../session/status";
import type { Theme } from "../theme/theme";
import { useTheme } from "../theme/use-theme";

/**
 * SessionStatusDot 是会话行左侧的执行状态点。
 *
 * 只表达执行状态：蓝=运行中、橙=等待批准、红=出错。idle 是默认状态，画点
 * 只会制造噪音；offline 不出现在列表里（离线节点的会话根本不加载），它属于
 * 会话详情页的状态条。未读点单独放在右侧，两个轴位置不同就不会撞色。
 */
export function SessionStatusDot({ state }: { state?: SessionState }) {
	const theme = useTheme();
	const color = state ? dotColor(theme, state) : null;
	if (!state || !color) return null;

	return (
		<View
			style={[styles.dot, { backgroundColor: color }]}
			accessibilityLabel={statusLabel(state)}
		/>
	);
}

/** UnreadDot 是时间右侧的未读圆点：有已完成但没看的内容。 */
export function UnreadDot() {
	const theme = useTheme();
	return (
		<View
			style={[styles.unread, { backgroundColor: theme.colors.accent }]}
			accessibilityLabel="未读"
		/>
	);
}

function dotColor(theme: Theme, state: SessionState): string | null {
	switch (state) {
		case "running":
			return theme.colors.accent;
		case "waiting_permission":
			// 用警告边框色而不是正文警告色：后者偏暗，小圆点上不够醒目。
			return theme.colors.warningBorder;
		case "error":
			return theme.colors.danger;
		default:
			return null;
	}
}

const styles = StyleSheet.create({
	dot: { width: 7, height: 7, borderRadius: 4 },
	unread: { width: 7, height: 7, borderRadius: 4 },
});
