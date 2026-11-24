import type { SessionState } from "@lework/api";
import { useEffect, useRef } from "react";
import { Animated, StyleSheet, View } from "react-native";

import { statusLabel } from "../session/status";
import { useTheme } from "../theme/use-theme";

/**
 * SessionStatusMark 是会话行左侧的执行状态标记。
 *
 * running 用一排色块轮动表示「还在跑」，waiting 用同款色块慢速呼吸提示需要人
 * 动手；静态圆点只留给 error。idle 是默认状态，画点只会制造噪音；offline 不
 * 出现在列表里（离线节点的会话根本不加载），它属于会话详情页的状态条。
 * running 跟随节点颜色，一眼对上是哪台电脑；waiting/error 保留语义色，状态
 * 比机器身份更需要被看见。未读点单独放在右侧，两个轴位置不同就不会撞色。
 */
export function SessionStatusMark({
	state,
	color,
}: {
	state?: SessionState;
	/** running 的主色；传节点色，未设色时回退主题 accent。 */
	color?: string | null;
}) {
	const theme = useTheme();
	if (!state) return null;
	if (state === "running") {
		return <AnimatedMark state={state} color={color ?? theme.colors.accent} />;
	}
	if (state === "waiting_permission") {
		// 警告边框色比正文警告色亮，小色块上更醒目。
		return <AnimatedMark state={state} color={theme.colors.warningBorder} />;
	}
	if (state === "error") {
		return (
			<View style={styles.slot} accessible accessibilityLabel={statusLabel(state)}>
				<View style={[styles.dot, { backgroundColor: theme.colors.danger }]} />
			</View>
		);
	}
	return null;
}

const BLOCK_COUNT = 3;

/**
 * AnimatedMark 渲染 running/waiting 的色块动画。
 *
 * 单独抽组件是为了让 hooks 只在动画出现时挂载：idle/error 的行不启动循环。
 * 列表里通常只有少量会话在跑，每个动画都走 native driver。
 */
function AnimatedMark({
	state,
	color,
}: {
	state: "running" | "waiting_permission";
	color: string;
}) {
	const values = useRef(Array.from({ length: BLOCK_COUNT }, () => new Animated.Value(0.3))).current;

	useEffect(() => {
		const loops = values.map((value, index) =>
			Animated.loop(
				state === "running"
					? Animated.sequence([
							// 相位错开，高亮从左往右跑。
							Animated.delay(index * 150),
							Animated.timing(value, {
								toValue: 1,
								duration: 280,
								useNativeDriver: true,
							}),
							Animated.timing(value, {
								toValue: 0.3,
								duration: 280,
								useNativeDriver: true,
							}),
							Animated.delay((BLOCK_COUNT - 1 - index) * 150),
						])
					: // 等待是同起同落的慢呼吸，和 running 的流动区分开。
						Animated.sequence([
							Animated.timing(value, {
								toValue: 1,
								duration: 900,
								useNativeDriver: true,
							}),
							Animated.timing(value, {
								toValue: 0.35,
								duration: 900,
								useNativeDriver: true,
							}),
						]),
			),
		);
		for (const loop of loops) loop.start();
		return () => {
			for (const loop of loops) loop.stop();
		};
	}, [state, values]);

	return (
		<View style={styles.slot} accessible accessibilityLabel={statusLabel(state)}>
			<View style={styles.blocks}>
				{values.map((value, index) => (
					<Animated.View
						// biome-ignore lint/suspicious/noArrayIndexKey: 三个色块是同一组动画的固定相位
						key={`block-${index}`}
						style={[styles.block, { backgroundColor: color, opacity: value }]}
					/>
				))}
			</View>
		</View>
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

const styles = StyleSheet.create({
	// 固定槽位：静态点与色块占同样宽度，列表标题不会因为状态切换而左右跳。
	slot: { width: 18, height: 10, alignItems: "center", justifyContent: "center" },
	blocks: { flexDirection: "row", alignItems: "center", gap: 2.5 },
	block: { width: 4, height: 9, borderRadius: 1.5 },
	dot: { width: 7, height: 7, borderRadius: 4 },
	unread: { width: 7, height: 7, borderRadius: 4 },
});
