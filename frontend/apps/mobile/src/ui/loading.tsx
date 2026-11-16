import { useEffect, useRef } from "react";
import { Animated, StyleSheet, Text, View, type ViewStyle } from "react-native";

import type { Theme } from "../theme/theme";
import { useTheme, useThemedStyles } from "../theme/use-theme";
import { BrandMark } from "./brand";

/**
 * BrandSpinner 是系统 ActivityIndicator 的品牌替代。
 *
 * Web 的加载语言是“细线 + 主色”，这里用一圈主色弧线保持一致；尺寸与
 * ActivityIndicator 接近，方便逐处替换而不改变布局。
 */
export function BrandSpinner({
	size = 18,
	color,
	style,
}: {
	size?: number;
	color?: string;
	style?: ViewStyle;
}) {
	const { colors } = useTheme();
	const rotation = useRef(new Animated.Value(0)).current;

	useEffect(() => {
		const loop = Animated.loop(
			Animated.timing(rotation, {
				toValue: 1,
				duration: 900,
				useNativeDriver: true,
			}),
		);
		loop.start();
		return () => loop.stop();
	}, [rotation]);

	return (
		<Animated.View
			accessibilityRole="progressbar"
			style={[
				{
					width: size,
					height: size,
					borderRadius: size / 2,
					borderWidth: Math.max(2, Math.round(size / 9)),
					borderColor: color ?? colors.primarySoft,
					borderTopColor: color ?? colors.action,
					transform: [
						{
							rotate: rotation.interpolate({
								inputRange: [0, 1],
								outputRange: ["0deg", "360deg"],
							}),
						},
					],
				},
				style,
			]}
		/>
	);
}

/**
 * LoadingDots 对齐 Web 的三点等待动画（TypingIndicator）：
 * 三个圆点依次呼吸，节奏 0 / 200 / 400ms。
 */
export function LoadingDots({
	color,
	dotSize = 6,
	gap = 5,
	style,
}: {
	color?: string;
	dotSize?: number;
	gap?: number;
	style?: ViewStyle;
}) {
	const { colors } = useTheme();
	const values = useRef([
		new Animated.Value(0.35),
		new Animated.Value(0.35),
		new Animated.Value(0.35),
	]).current;

	useEffect(() => {
		const loops = values.map((value, index) =>
			Animated.loop(
				Animated.sequence([
					Animated.delay(index * 200),
					Animated.timing(value, { toValue: 1, duration: 320, useNativeDriver: true }),
					Animated.timing(value, { toValue: 0.35, duration: 320, useNativeDriver: true }),
					Animated.delay((2 - index) * 200),
				]),
			),
		);
		for (const loop of loops) loop.start();
		return () => {
			for (const loop of loops) loop.stop();
		};
	}, [values]);

	return (
		<View style={[styles.dots, { gap }, style]} accessibilityLabel="正在加载">
			{values.map((value, index) => (
				<Animated.View
					// 顺序固定，三点是同一组动画的三个相位。
					// biome-ignore lint/suspicious/noArrayIndexKey: 三点相位固定，索引即身份
					key={`dot-${index}`}
					style={{
						width: dotSize,
						height: dotSize,
						borderRadius: dotSize / 2,
						backgroundColor: color ?? colors.textMuted,
						opacity: value,
					}}
				/>
			))}
		</View>
	);
}

/** Skeleton 用低对比度脉冲占位，避免整页只剩一个转圈。 */
export function Skeleton({
	width,
	height,
	radius,
	style,
}: {
	width?: number | `${number}%`;
	height: number;
	radius?: number;
	style?: ViewStyle;
}) {
	const { colors, radii } = useTheme();
	const opacity = useRef(new Animated.Value(0.5)).current;

	useEffect(() => {
		const loop = Animated.loop(
			Animated.sequence([
				Animated.timing(opacity, { toValue: 1, duration: 700, useNativeDriver: true }),
				Animated.timing(opacity, { toValue: 0.5, duration: 700, useNativeDriver: true }),
			]),
		);
		loop.start();
		return () => loop.stop();
	}, [opacity]);

	return (
		<Animated.View
			style={[
				{
					width: width ?? "100%",
					height,
					borderRadius: radius ?? radii.sm,
					backgroundColor: colors.surfaceMuted,
					opacity,
				},
				style,
			]}
		/>
	);
}

/** LoadingScreen 用于首屏/整页加载，品牌标识 + 三点动画。 */
export function LoadingScreen({ text = "正在加载…" }: { text?: string }) {
	const styles = useThemedStyles(createStyles);
	return (
		<View style={styles.screen}>
			<BrandMark size={56} />
			<LoadingDots style={styles.dotsGap} />
			<Text style={styles.text}>{text}</Text>
		</View>
	);
}

/** InlineLoading 是行内“spinner + 文案”的统一写法。 */
export function InlineLoading({ text = "加载中…" }: { text?: string }) {
	const styles = useThemedStyles(createStyles);
	return (
		<View style={styles.inline}>
			<BrandSpinner size={16} />
			<Text style={styles.inlineText}>{text}</Text>
		</View>
	);
}

const styles = StyleSheet.create({
	dots: { flexDirection: "row", alignItems: "center" },
});

function createStyles(theme: Theme) {
	return StyleSheet.create({
		screen: { flex: 1, alignItems: "center", justifyContent: "center", gap: 14 },
		dotsGap: { marginTop: 2 },
		text: { fontSize: 13, color: theme.colors.textMuted },
		inline: { flexDirection: "row", alignItems: "center", gap: 8 },
		inlineText: { fontSize: 13, color: theme.colors.textMuted },
	});
}
