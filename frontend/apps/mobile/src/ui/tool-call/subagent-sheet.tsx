import type { SubagentView, ToolBlock } from "@lework/tool-view";
import { router } from "expo-router";
import { Modal, Pressable, ScrollView, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { sessionRoute } from "../../config";
import { useToolStyles } from "./font-scale";
import { RawJson } from "./raw-json";
import { ToolBlocks } from "./tool-blocks";

/**
 * SubagentSheet 用全屏页承载子代理详情。
 *
 * prompt 和结果经常是整段文本，塞进气泡只能看到几行；全屏页可滚动、可复制，
 * 有子会话 id 时还能直接跳过去继续看它的对话。
 */
export function SubagentSheet({
	visible,
	detail,
	tool,
	status,
	blocks,
	raw,
	nodeID,
	onClose,
}: {
	visible: boolean;
	detail: SubagentView;
	tool: string;
	status: string;
	blocks: ToolBlock[];
	raw: string;
	nodeID: string;
	onClose: () => void;
}) {
	const styles = useToolStyles();
	const insets = useSafeAreaInsets();
	const meta = [tool, status, detail.duration ?? "", detail.background ? "后台运行" : ""]
		.filter((part) => part !== "")
		.join(" · ");

	const openChild = () => {
		if (!detail.sessionID) return;
		onClose();
		router.push(sessionRoute(nodeID, detail.sessionID));
	};

	return (
		<Modal visible={visible} animationType="slide" onRequestClose={onClose}>
			<View style={[styles.sheetScreen, { paddingTop: insets.top }]}>
				<View style={styles.sheetHeader}>
					<View style={styles.sheetHeading}>
						<Text style={styles.sheetTitle}>子代理</Text>
						<Text style={styles.sheetSubtitle} numberOfLines={1}>
							{detail.agent || "agent"}
						</Text>
					</View>
					<Pressable onPress={onClose} accessibilityLabel="关闭子代理详情">
						<Text style={styles.sheetClose}>关闭</Text>
					</Pressable>
				</View>

				<ScrollView
					style={styles.sheetScroll}
					contentContainerStyle={[styles.sheetContent, { paddingBottom: insets.bottom + 24 }]}
				>
					<View style={styles.sheetMeta}>
						<Text style={styles.sheetMetaText}>{meta}</Text>
						{detail.description ? (
							<Text style={styles.sheetDescription}>{detail.description}</Text>
						) : null}
					</View>

					{detail.sessionID ? (
						<Pressable style={styles.primaryButton} onPress={openChild}>
							<Text style={styles.primaryButtonText}>打开子会话</Text>
						</Pressable>
					) : null}

					<ToolBlocks blocks={blocks} />
					<RawJson text={raw} />
				</ScrollView>
			</View>
		</Modal>
	);
}
