import { useCallback, useEffect, useState } from "react";
import {
	Keyboard,
	type KeyboardEvent,
	Platform,
	Pressable,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { keyboardOverlap } from "../composer/keyboard";
import type { Theme } from "../theme/theme";
import { useTheme, useThemedStyles } from "../theme/use-theme";
import { BrandSpinner } from "./loading";

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
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const disabled = props.target === null;
	const canSend = !disabled && !props.sending && props.value.trim() !== "";
	const baseBottom = Math.max(insets.bottom, 8);

	// 输入法让出的额外高度。不用 KeyboardAvoidingView：它只在键盘事件到来时更新，
	// 而页面常常是在键盘已经打开时挂载的（首页发送后直接 router.push 到会话页），
	// 这时不会再有 didShow，输入栏就会一直压在输入法下面。直接读当前键盘 metrics，
	// 并在输入框聚焦时补同步，与页面挂载时机无关。
	const [keyboardInset, setKeyboardInset] = useState(0);
	const applyKeyboardHeight = useCallback(
		(keyboardHeight: number | undefined) => {
			setKeyboardInset(
				keyboardOverlap({
					keyboardHeight,
					baseBottom,
					platform: Platform.OS,
				}),
			);
		},
		[baseBottom],
	);
	const syncKeyboardInset = useCallback(() => {
		applyKeyboardHeight(Keyboard.metrics()?.height);
	}, [applyKeyboardHeight]);

	useEffect(() => {
		const handleShow = (event: KeyboardEvent) => {
			// 用事件里的高度：iOS 的 willShow 早于全局 metrics 刷新，读缓存会晚一帧。
			// iOS 按键盘动画时长同步推高；Android 事件时长为 0，直接落位。
			Keyboard.scheduleLayoutAnimation(event);
			applyKeyboardHeight(event.endCoordinates.height);
		};
		const showEvent = Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow";
		const hideEvent = Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide";
		const subscriptions = [
			Keyboard.addListener(showEvent, handleShow),
			Keyboard.addListener(hideEvent, () => setKeyboardInset(0)),
		];
		// 挂载时键盘可能已经打开，先按当前状态对齐一次。
		applyKeyboardHeight(Keyboard.metrics()?.height);
		return () => {
			for (const subscription of subscriptions) subscription.remove();
		};
	}, [applyKeyboardHeight]);

	return (
		<View style={[styles.container, { paddingBottom: baseBottom + keyboardInset }]}>
			{props.disabledReason ? <Text style={styles.reason}>{props.disabledReason}</Text> : null}
			{props.target && props.showChip !== false ? (
				<Pressable disabled={!props.onPickTarget} onPress={props.onPickTarget} style={styles.chip}>
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
					onFocus={syncKeyboardInset}
					editable={!disabled}
					multiline
					placeholder={disabled ? "暂不可发送" : "说点什么…"}
					// 16px 起步：iOS 上更小的字号会在聚焦时自动放大页面。
					placeholderTextColor={theme.colors.placeholder}
					selectionColor={theme.colors.accent}
				/>
				<Pressable
					disabled={!canSend}
					onPress={() => void props.onSend()}
					style={[styles.send, !canSend && styles.sendDisabled]}
				>
					{props.sending ? (
						<BrandSpinner color={theme.colors.actionText} />
					) : (
						<Text style={styles.sendText}>发送</Text>
					)}
				</Pressable>
			</View>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		container: {
			borderTopWidth: 1,
			borderTopColor: colors.border,
			backgroundColor: colors.surface,
			paddingHorizontal: 12,
			paddingTop: 8,
			gap: 6,
		},
		reason: { fontSize: 12, color: colors.warning },
		chip: { alignSelf: "flex-start", paddingVertical: 2 },
		chipText: { fontSize: 12, color: colors.textSecondary, maxWidth: 280 },
		row: { flexDirection: "row", alignItems: "flex-end", gap: 8 },
		input: {
			flex: 1,
			minHeight: 40,
			maxHeight: 120,
			borderWidth: 1,
			borderColor: colors.inputBorder,
			borderRadius: 20,
			paddingHorizontal: 14,
			paddingVertical: 10,
			fontSize: 16,
			backgroundColor: colors.inputBackground,
			color: colors.text,
		},
		send: {
			height: 40,
			paddingHorizontal: 16,
			borderRadius: 20,
			backgroundColor: colors.action,
			alignItems: "center",
			justifyContent: "center",
		},
		sendDisabled: { opacity: 0.4 },
		sendText: { color: colors.actionText, fontWeight: "600" },
	});
}
