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

/**
 * clock 是所有扫光行共用的进度。
 *
 * 每行各起一条 loop 时，屏幕上有几个运行中的会话就有几条原生动画；共用一条
 * 后只剩一条，而且各行进度同步，读起来是“同一批任务在跑”而不是一堆各扫各的
 * 光带。无人订阅时停掉，空闲列表不烧电。
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
 * running/waiting 用一条光带扫过整行，running 跟随节点色、waiting 用警告色；
 * error 铺静态淡红底——会动的底色会被读成“还在跑”。行本身只需 `overflow:
 * hidden`，光带不会盖住文字（层级在内容之下，且不接收触摸）。
 */
export function RowSweep({ state, color }: { state?: SessionState | null; color?: string | null }) {
	const theme = useTheme();
	const resolved = state ?? null;
	const kind = rowSweepKind(resolved);
	if (!kind) return null;

	if (kind === "static") {
		return (
			<View
				style={[StyleSheet.absoluteFill, { backgroundColor: theme.colors.dangerSurface }]}
				accessible
				accessibilityLabel={statusLabel(resolved)}
				pointerEvents="none"
			/>
		);
	}

	return (
		<SweepBand
			label={statusLabel(resolved)}
			tint={state === "running" ? (color ?? theme.colors.accent) : theme.colors.warningBorder}
		/>
	);
}

function SweepBand({ label, tint }: { label: string; tint: string }) {
	const [width, setWidth] = useState(0);
	const reduceMotion = useReduceMotion();
	const band = Math.round(width * BAND_RATIO);

	useEffect(() => {
		if (reduceMotion) return;
		return subscribeClock();
	}, [reduceMotion]);

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
						opacity: PEAK_OPACITY,
						// 减少动效时把光带静止在中间，状态仍然看得见。
						transform: [
							{
								translateX: reduceMotion
									? (width - band) / 2
									: clock.interpolate({ inputRange: [0, 1], outputRange: [-band, width] }),
							},
						],
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
