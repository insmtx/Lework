import {
	ActivityIndicator,
	KeyboardAvoidingView,
	Platform,
	Pressable,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/** ComposerTarget 是当前输入要发往的项目/节点。 */
export interface ComposerTarget {
	projectKey: string;
	projectName: string;
	nodeID: string | null;
	nodeName?: string;
	/** 会话页只知道节点，传 false 时 chip 只显示 projectName。 */
	showNode?: boolean;
}

export interface GlobalComposerProps {
	/** null 表示没有可用目标（没有项目或没有在线节点）。 */
	target: ComposerTarget | null;
	disabledReason?: string;
	value: string;
	onChangeText: (text: string) => void;
	onSend: () => Promise<void>;
	sending: boolean;
	/** 非会话页可切项目/节点；会话页不传，目标只读。 */
	onPickTarget?: () => void;
	/** 会话页标题已经写明了对象，再显示 chip 就是重复信息。 */
	showChip?: boolean;
}

/**
 * GlobalComposer 是全页面常驻的底部输入栏（豆包风格）。
 *
 * 它不拥有发送逻辑：父页面决定目标节点与失败后是否保留草稿，这样
 * 「发送失败不清空」只在一个地方实现。
 */
export function GlobalComposer(props: GlobalComposerProps) {
	const insets = useSafeAreaInsets();
	const disabled = props.target === null;
	const canSend = !disabled && !props.sending && props.value.trim() !== "";

	return (
		<KeyboardAvoidingView
			behavior={Platform.OS === "ios" ? "padding" : undefined}
			keyboardVerticalOffset={80}
		>
			<View style={[styles.container, { paddingBottom: Math.max(insets.bottom, 8) }]}>
				{props.disabledReason ? <Text style={styles.reason}>{props.disabledReason}</Text> : null}
				{props.target && props.showChip !== false ? (
					<Pressable
						disabled={!props.onPickTarget}
						onPress={props.onPickTarget}
						style={styles.chip}
					>
						<Text style={styles.chipText} numberOfLines={1}>
							{props.target.projectName}
							{props.target.showNode === false
								? ""
								: props.target.nodeID
									? ` · ${props.target.nodeName ?? props.target.nodeID}`
									: " · 节点离线"}
							{props.onPickTarget ? "  ▾" : ""}
						</Text>
					</Pressable>
				) : null}
				<View style={styles.row}>
					<TextInput
						style={styles.input}
						value={props.value}
						onChangeText={props.onChangeText}
						editable={!disabled}
						multiline
						placeholder={disabled ? "暂不可发送" : "说点什么…"}
						// 16px 起步：iOS 上更小的字号会在聚焦时自动放大页面。
						placeholderTextColor="#94a3b8"
					/>
					<Pressable
						disabled={!canSend}
						onPress={() => void props.onSend()}
						style={[styles.send, !canSend && styles.sendDisabled]}
					>
						{props.sending ? (
							<ActivityIndicator color="#fff" />
						) : (
							<Text style={styles.sendText}>发送</Text>
						)}
					</Pressable>
				</View>
			</View>
		</KeyboardAvoidingView>
	);
}

const styles = StyleSheet.create({
	container: {
		borderTopWidth: 1,
		borderTopColor: "#e2e8f0",
		backgroundColor: "#fff",
		paddingHorizontal: 12,
		paddingTop: 8,
		gap: 6,
	},
	reason: { fontSize: 12, color: "#b45309" },
	chip: { alignSelf: "flex-start", paddingVertical: 2 },
	chipText: { fontSize: 12, color: "#475569", maxWidth: 280 },
	row: { flexDirection: "row", alignItems: "flex-end", gap: 8 },
	input: {
		flex: 1,
		minHeight: 40,
		maxHeight: 120,
		borderWidth: 1,
		borderColor: "#e2e8f0",
		borderRadius: 20,
		paddingHorizontal: 14,
		paddingVertical: 10,
		fontSize: 16,
		backgroundColor: "#f8fafc",
	},
	send: {
		height: 40,
		paddingHorizontal: 16,
		borderRadius: 20,
		backgroundColor: "#0f172a",
		alignItems: "center",
		justifyContent: "center",
	},
	sendDisabled: { opacity: 0.4 },
	sendText: { color: "#fff", fontWeight: "600" },
});
