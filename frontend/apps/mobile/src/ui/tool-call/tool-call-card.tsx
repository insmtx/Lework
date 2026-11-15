import type { ContentPart } from "@lework/api";
import { toolView } from "@lework/tool-view";
import { useMemo, useState } from "react";
import { Pressable, Text, View } from "react-native";

import { useTheme } from "../../theme/use-theme";
import { ToolFontScaleProvider, useToolStyles } from "./font-scale";
import { RawJson } from "./raw-json";
import { SubagentSheet } from "./subagent-sheet";
import { ToolBlocks } from "./tool-blocks";

/**
 * ToolCallCard 是一条工具调用的入口。
 *
 * 普通工具点开在原地展开，内容由展示模型决定；子代理直接弹全屏页，因为它的
 * prompt 和结果长度不适合放在消息流里。
 */
export function ToolCallCard({
	part,
	defaultExpanded,
	fontScale,
	nodeID,
}: {
	part: ContentPart;
	defaultExpanded: boolean;
	/** 会话字号倍率；工具输出与正文使用同一档，避免出现两套字号。 */
	fontScale: number;
	nodeID: string;
}) {
	const theme = useTheme();
	const styles = useToolStyles();
	const view = useMemo(() => toolView(part), [part]);
	const [expanded, setExpanded] = useState(defaultExpanded);
	const [sheetOpen, setSheetOpen] = useState(false);

	if (!view) return null;

	const detail = view.subagent;
	const expandable = detail ? true : view.blocks.length > 0 || view.raw !== "";
	const toneColor =
		view.tone === "error"
			? theme.colors.danger
			: view.tone === "ok"
				? theme.colors.success
				: theme.colors.textSecondary;

	const toggle = () => {
		if (!expandable) return;
		if (detail) {
			setSheetOpen(true);
			return;
		}
		setExpanded((open) => !open);
	};

	return (
		<ToolFontScaleProvider scale={fontScale}>
			<View style={styles.card}>
				<Pressable
					style={styles.header}
					disabled={!expandable}
					onPress={toggle}
					accessibilityLabel={detail ? "打开子代理详情" : "展开或收起工具调用"}
				>
					<Text style={styles.chevron}>
						{expandable ? (detail ? "›" : expanded ? "▾" : "▸") : "•"}
					</Text>
					<Text style={styles.toolName} numberOfLines={1}>
						{view.tool}
					</Text>
					<Text style={[styles.status, { color: toneColor }]}>{view.status}</Text>
				</Pressable>

				{(detail || !expanded) && view.summary !== "" ? (
					<Text style={styles.summary} numberOfLines={1}>
						{view.summary}
					</Text>
				) : null}

				{!detail && expanded ? (
					<View style={styles.body}>
						<ToolBlocks blocks={view.blocks} />
						<RawJson text={view.raw} />
					</View>
				) : null}
			</View>

			{detail ? (
				<SubagentSheet
					visible={sheetOpen}
					detail={detail}
					tool={view.tool}
					status={view.status}
					blocks={view.blocks}
					raw={view.raw}
					nodeID={nodeID}
					onClose={() => setSheetOpen(false)}
				/>
			) : null}
		</ToolFontScaleProvider>
	);
}
