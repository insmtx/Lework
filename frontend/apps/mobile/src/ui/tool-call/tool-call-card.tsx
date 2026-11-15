import type { ContentPart } from "@lework/api";
import { toolView } from "@lework/tool-view";
import { useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, Pressable, Text, View } from "react-native";

import { useTheme } from "../../theme/use-theme";
import { resolveExpanded } from "./collapse";
import { ToolFontScaleProvider, useToolStyles } from "./font-scale";
import { RawJson } from "./raw-json";
import { SubagentSheet } from "./subagent-sheet";
import { ToolBlocks } from "./tool-blocks";

/**
 * ToolCallCard 是一条工具调用的入口。
 *
 * 普通工具点开在原地展开，内容由展示模型决定；子代理直接弹全屏页，因为它的
 * prompt 和结果长度不适合放在消息流里。运行中的调用自动展开并显示 loading，
 * 完成且后面出现新内容后自动折叠，把空间让给下一段。
 */
export function ToolCallCard({
	part,
	defaultExpanded,
	fontScale,
	isLast,
	nodeID,
}: {
	part: ContentPart;
	defaultExpanded: boolean;
	/** 会话字号倍率；工具输出与正文使用同一档，避免出现两套字号。 */
	fontScale: number;
	/** 是否为本轮最后一段内容；完成后仍处于末尾时先保持展开。 */
	isLast: boolean;
	nodeID: string;
}) {
	const theme = useTheme();
	const styles = useToolStyles();
	const view = useMemo(() => toolView(part), [part]);
	const running = view?.tone === "running";
	const [expanded, setExpanded] = useState(defaultExpanded || running);
	const [sheetOpen, setSheetOpen] = useState(false);
	// 手动开合优先于自动行为：用户收起后不该被流式更新重新弹开。
	const manual = useRef(false);
	const ranDuringMount = useRef(running);

	useEffect(() => {
		if (!view) return;
		if (view.tone === "running") ranDuringMount.current = true;
		setExpanded((current) =>
			resolveExpanded({
				tone: view.tone,
				hasFollowingContent: !isLast,
				preferExpanded: defaultExpanded,
				manual: manual.current,
				expanded: current,
				ranDuringMount: ranDuringMount.current,
			}),
		);
	}, [view, isLast, defaultExpanded]);

	if (!view) return null;

	const detail = view.subagent;
	const expandable = detail ? true : view.blocks.length > 0 || view.raw !== "";
	const toneColor =
		view.tone === "error"
			? theme.colors.danger
			: view.tone === "ok"
				? theme.colors.success
				: theme.colors.accent;

	const toggle = () => {
		if (!expandable) return;
		manual.current = true;
		if (detail) {
			setSheetOpen(true);
			return;
		}
		setExpanded((open) => !open);
	};

	return (
		<ToolFontScaleProvider scale={fontScale}>
			<View style={[styles.card, running && styles.cardRunning]}>
				<Pressable
					style={styles.header}
					disabled={!expandable}
					onPress={toggle}
					accessibilityLabel={
						detail
							? "打开子代理详情"
							: running
								? "工具运行中，展开或收起输出"
								: "展开或收起工具调用"
					}
				>
					<Text style={styles.chevron}>
						{expandable ? (detail ? "›" : expanded ? "▾" : "▸") : "•"}
					</Text>
					<Text style={styles.toolName} numberOfLines={1}>
						{view.tool}
					</Text>
					<Text style={[styles.status, running && styles.statusRunning, { color: toneColor }]}>
						{view.status}
					</Text>
					{running ? (
						<ActivityIndicator size="small" color={theme.colors.accent} style={styles.spinner} />
					) : null}
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
