import { StyleSheet, View } from "react-native";

import { useTheme } from "../theme/use-theme";

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

const styles = StyleSheet.create({
	unread: { width: 7, height: 7, borderRadius: 4 },
});
