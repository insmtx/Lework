import { useState } from "react";
import { Pressable, Text, View } from "react-native";

import { clipText } from "../../session/tools/values";
import { useThemedStyles } from "../../theme/use-theme";
import { createStyles } from "./styles";

const RAW_LIMIT = 6000;

/**
 * RawJson 默认收起原始 state。
 *
 * 它是排查入口而不是主视图：工具没有专属渲染器时也仍然能找到完整字段，
 * 但正常阅读不会被 JSON 打断。
 */
export function RawJson({ text }: { text: string }) {
	const styles = useThemedStyles(createStyles);
	const [open, setOpen] = useState(false);

	if (text === "") return null;

	return (
		<View style={styles.raw}>
			<Pressable onPress={() => setOpen((value) => !value)} accessibilityLabel="展开或收起原始数据">
				<Text style={styles.rawToggle}>{open ? "▾" : "▸"} 原始数据</Text>
			</Pressable>
			{open ? (
				<Text style={styles.rawBody} selectable>
					{clipText(text, RAW_LIMIT)}
				</Text>
			) : null}
		</View>
	);
}
