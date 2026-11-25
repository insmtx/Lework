import type { SessionState } from "@lework/api";
import { LinearGradient } from "expo-linear-gradient";
import { useEffect, useState } from "react";
import { AccessibilityInfo, Animated, Easing, StyleSheet, View } from "react-native";

import { rowSweepKind, statusLabel } from "../session/status";
import { useTheme } from "../theme/use-theme";

/** 一次扫光的时长，与 Web 控制台的 2.4s 对齐。 */
const SWEEP_DURATION = 2400;

/** 光带占行宽的比例：太宽像整行变色，太窄又看不出在动。 */
const BAND_RATIO = 0.8;

/** 光带峰值透明度，对齐 Web 控制台 30% 的色带浓度。 */
const PEAK_OPACITY = 0.3;

/** 呼吸的两端：低到像余烬，高到接近扫光峰值，整条带子仍在可读范围内。 */
const BREATH_LOW = 0.1;
const BREATH_HIGH = 0.4;

/**
 * clock 是所有扫光行共用的进度。
 *
 * 每行各起一条 loop 时，屏幕上有几个运行中的会话就有几条原生动画；共用一条
 * 后只剩一条，而且各行进度同步，读起来是“同一批任务在跑”而不是一堆各扫各的
 * 光带。呼吸状态也挂在这条时钟上，不再多一条动画。无人订阅时停掉，空闲列表
 * 不烧电。
 */
const clock = new Animated.Value(0);
let clockUsers = 0;
let clockLoop: Animated.CompositeAnimation | null = null;

function subscribeClock(): () => void {
	if (clockUsers === 0) {
		clock.setValue(0);
		clockLoop = Animated.loop(
			Animated.timing(clock, {
				toValue: 1,
				duration: SWEEP_DURATION,
				easing: Easing.linear,
				useNativeDriver: true,
			}),
		);
		clockLoop.start();
	}
	clockUsers += 1;
	return () => {
		clockUsers -= 1;
		if (clockUsers > 0) return;
		clockLoop?.stop();
		clockLoop = null;
	};
}

/**
 * RowSweep 给会话行铺整行状态底色，替代原先行首的小色块。
 *
 * 三种状态共用同一条光带，像同一次执行的三帧：running 扫过整行并跟随节点色；
 * waiting 停在行尾（跑到终点等你处理）；error 停在行中间并呼吸（被打断在那里）。
 * 行本身只需 `overflow: hidden`，光带不会盖住文字（层级在内容之下，也不接收
 * 触摸）。
 */
export function RowSweep({ state, color }: { state?: SessionState | null; color?: string | null }) {
	const theme = useTheme();
	const resolved = state ?? null;
	const mode = rowSweepKind(resolved);
	if (!mode) return null;

	const tint =
		resolved === "running"
			? (color ?? theme.colors.accent)
			: resolved === "waiting_permission"
				? theme.colors.warningBorder
				: theme.colors.danger;

	return <SweepBand label={statusLabel(resolved)} tint={tint} mode={mode} />;
}

function SweepBand({
	label,
	tint,
	mode,
}: {
	label: string;
	tint: string;
	mode: "sweep" | "rest" | "breathe";
}) {
	const [width, setWidth] = useState(0);
	const reduceMotion = useReduceMotion();
	const band = Math.round(width * BAND_RATIO);
	// 停在行尾不需要时钟；呼吸和扫光共用同一条。
	const needsClock = mode !== "rest" && !reduceMotion;

	useEffect(() => {
		if (!needsClock) return;
		return subscribeClock();
	}, [needsClock]);

	// 减少动效时，扫光也停在行尾，只靠颜色区分状态。
	const parked = mode !== "sweep" || reduceMotion;
	const offset = parked
		? mode === "breathe"
			? (width - band) / 2
			: width - band
		: clock.interpolate({ inputRange: [0, 1], outputRange: [-band, width] });
	const opacity =
		mode === "breathe" && !reduceMotion
			? // 三角波近似呼吸：匀速淡入淡出，不会像扫光那样被读成“在动”。
				clock.interpolate({
					inputRange: [0, 0.25, 0.5, 0.75, 1],
					outputRange: [BREATH_LOW, 0.25, BREATH_HIGH, 0.25, BREATH_LOW],
				})
			: PEAK_OPACITY;

	return (
		<View
			style={StyleSheet.absoluteFill}
			pointerEvents="none"
			accessible
			accessibilityLabel={label}
			onLayout={(event) => setWidth(event.nativeEvent.layout.width)}
		>
			{width > 0 ? (
				<Animated.View
					style={{
						position: "absolute",
						top: 0,
						bottom: 0,
						width: band,
						opacity,
						transform: [{ translateX: offset }],
					}}
				>
					{/* 同色两端全透明：用自己的色值而不是 transparent，避免插值偏黑。 */}
					<LinearGradient
						colors={[`${tint}00`, tint, `${tint}00`]}
						locations={[0, 0.5, 1]}
						start={{ x: 0, y: 0.5 }}
						end={{ x: 1, y: 0.5 }}
						style={StyleSheet.absoluteFill}
					/>
				</Animated.View>
			) : null}
		</View>
	);
}

/** useReduceMotion 跟随系统的“减少动态效果”设置。 */
function useReduceMotion(): boolean {
	const [enabled, setEnabled] = useState(false);

	useEffect(() => {
		let mounted = true;
		const apply = (value: boolean) => {
			if (mounted) setEnabled(value);
		};
		AccessibilityInfo.isReduceMotionEnabled()
			.then(apply)
			.catch(() => apply(false));
		const subscription = AccessibilityInfo.addEventListener("reduceMotionChanged", apply);
		return () => {
			mounted = false;
			subscription.remove();
		};
	}, []);

	return enabled;
}
