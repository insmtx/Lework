import { type ComponentRef, useCallback, useRef, useState } from "react";
import type {
	LayoutChangeEvent,
	NativeScrollEvent,
	NativeSyntheticEvent,
	ScrollView,
} from "react-native";

import { bottomOffset, followOffset, isFarFromBottom, isNearBottom } from "./scroll";

/** 超过这段时间没有新的滚动事件，就认为用户这一次拖动/惯性已经结束。 */
const GESTURE_IDLE_MS = 300;

/**
 * keepVisibleContent 交给 native 的 maintainVisibleContentPosition。
 *
 * 消息流不再虚拟化（见页面里的 ScrollView），但视口上方的行仍可能变高
 * （例如上方工具卡自动折叠）；开启后 native 在同一次布局提交里按第一个
 * 可见子视图的位移补正偏移，不经过 JS。iOS 与 Android 新架构都支持。
 */
export const keepVisibleContent = { minIndexForVisible: 0 } as const;

/** AutoFollowHandlers 是挂到列表上的滚动事件集合。 */
export interface AutoFollowHandlers {
	onContentSizeChange: (width: number, height: number) => void;
	onLayout: (event: LayoutChangeEvent) => void;
	onScroll: (event: NativeSyntheticEvent<NativeScrollEvent>) => void;
	onScrollBeginDrag: () => void;
	onScrollEndDrag: () => void;
	onMomentumScrollBegin: () => void;
	onMomentumScrollEnd: () => void;
}

/**
 * useAutoFollow 让消息流稳定地跟随底部。
 *
 * 消息流用普通 ScrollView 渲染，不用 FlatList：没有 getItemLayout 的虚拟列表
 * 会用「已测量行的平均高度」估算没渲染的行（视口上下的 spacer），而流式输出
 * 持续改变平均高度，spacer 就成倍伸缩，内容被顶上/顶下几十到几百像素，JS 再
 * 下一帧滚回来——表现就是来回跳。
 *
 * 贴底用 onContentSizeChange 给的真实内容高度算偏移，不用 scrollToEnd 的估算。
 * 首屏仍用 scrollToEnd：一次落到绝对底部，之后的内容增长交给真实高度逐帧追。
 *
 * 跟随状态只看用户发起的手势帧（拖动与其惯性），程序化贴底产生的帧不参与判断。
 * 手势可能漏发结束事件，用 300ms 静默看门狗兜底。
 *
 * 贴底只向底部追：内容高度变小时不主动回滚，交给系统把偏移夹回新的底部。
 */
export function useAutoFollow({
	following,
	setFollowing,
	onAtBottom,
}: {
	/** 父级持有的跟随状态；它同时驱动「有新内容」提示。 */
	following: boolean;
	setFollowing: (following: boolean) => void;
	/** 滚动落到贴底位置时回调；调用方需自行节流。 */
	onAtBottom?: () => void;
}) {
	const listRef = useRef<ComponentRef<typeof ScrollView>>(null);
	const contentHeight = useRef(0);
	const viewportHeight = useRef(0);
	// 离底部超过半屏时露出「回到底部」；滚动帧里更新，避免小幅上滑就弹按钮。
	const [farFromBottom, setFarFromBottom] = useState(false);
	// 同一帧内可能连续收到内容尺寸与滚动事件，同步镜像保证读到最新意图。
	const followingRef = useRef(following);
	followingRef.current = following;
	// 手指是否按在列表上；只用来避免用户拖动时抢滚动。
	const dragging = useRef(false);
	// 是否处于用户发起的滚动（拖动或惯性）；只有这些帧能改变跟随意图。
	const gesture = useRef(false);
	const lastScrollAt = useRef(0);
	// 最近一次已知的滚动位置（含程序化滚动），用来判断是否还需要往底部追。
	const lastOffset = useRef(0);
	// 下一次内容长过一屏时用 scrollToEnd 落到绝对底部（首屏与主动跳转）。
	const wantBottom = useRef(false);

	// native 可能漏发 onScrollEndDrag，用「一段时间没有滚动事件」兜底解除手势状态。
	const releaseStaleGesture = useCallback(() => {
		if (!gesture.current && !dragging.current) return;
		if (Date.now() - lastScrollAt.current < GESTURE_IDLE_MS) return;
		gesture.current = false;
		dragging.current = false;
	}, []);

	const applyScroll = useCallback(() => {
		releaseStaleGesture();
		if (!followingRef.current || dragging.current) return;
		if (viewportHeight.current <= 0 || contentHeight.current <= 0) return;
		// 内容长过一屏才消费：空列表和不足一屏的内容本来就贴着底部，不用滚。
		if (wantBottom.current && contentHeight.current > viewportHeight.current) {
			wantBottom.current = false;
			listRef.current?.scrollToEnd({ animated: false });
			return;
		}
		const target = bottomOffset(contentHeight.current, viewportHeight.current);
		const next = followOffset(target, lastOffset.current);
		if (next === null) return;
		// 自动贴底不做动画：流式增量会让动画不断被重启，而且动画的中间帧
		// 会被误当成用户滚动。瞬时落到底反而更稳。
		listRef.current?.scrollTo({ y: next, animated: false });
	}, [releaseStaleGesture]);

	const applyFollowing = useCallback(
		(next: boolean) => {
			if (followingRef.current === next) return;
			followingRef.current = next;
			setFollowing(next);
		},
		[setFollowing],
	);

	const handleScroll = useCallback(
		(event: NativeSyntheticEvent<NativeScrollEvent>) => {
			// 先按上一帧的时间判断手势是否已经静默结束，再刷新时间戳。
			releaseStaleGesture();
			const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
			lastOffset.current = contentOffset.y;
			lastScrollAt.current = Date.now();
			const metrics = {
				offsetY: contentOffset.y,
				contentHeight: contentSize.height,
				viewportHeight: layoutMeasurement.height,
			};
			const atBottom = isNearBottom(metrics);
			setFarFromBottom(isFarFromBottom(metrics));
			// 程序化贴底永远落在底部；首屏测量中的中间帧不能把跟随关掉。
			if (gesture.current) applyFollowing(atBottom);
			if (atBottom) onAtBottom?.();
		},
		[applyFollowing, onAtBottom, releaseStaleGesture],
	);

	const handlers: AutoFollowHandlers = {
		onContentSizeChange: (_width, height) => {
			contentHeight.current = height;
			applyScroll();
		},
		onLayout: (event) => {
			viewportHeight.current = event.nativeEvent.layout.height;
			// 键盘、旋转或输入框长高后重新贴底，但不打断用户正在进行的拖动。
			applyScroll();
		},
		onScroll: handleScroll,
		onScrollBeginDrag: () => {
			dragging.current = true;
			gesture.current = true;
			// 用户已经接管滚动，别再把他往底部拽。
			wantBottom.current = false;
			lastScrollAt.current = Date.now();
		},
		onScrollEndDrag: () => {
			dragging.current = false;
			// 惯性可能紧接着开始，手势状态在惯性结束或静默兜底时才解除。
			lastScrollAt.current = Date.now();
		},
		onMomentumScrollBegin: () => {
			// 惯性开始意味着拖动已经结束，只保留最终的贴底对齐。
			dragging.current = false;
			gesture.current = true;
			lastScrollAt.current = Date.now();
		},
		onMomentumScrollEnd: () => {
			gesture.current = false;
			lastScrollAt.current = Date.now();
			applyScroll();
		},
	};

	/** follow 恢复跟随并立即落到绝对底部。 */
	const follow = useCallback(() => {
		// 主动回到最新一条时，之前的拖动与未消化的落底意图都要让位。
		dragging.current = false;
		gesture.current = false;
		applyFollowing(true);
		wantBottom.current = true;
		setFarFromBottom(false);
		applyScroll();
	}, [applyFollowing, applyScroll]);

	/** reset 在切换会话时调用：恢复跟随，等首次内容长过后由尺寸事件落到底部。 */
	const reset = useCallback(() => {
		dragging.current = false;
		gesture.current = false;
		lastScrollAt.current = 0;
		// 新会话的滚动位置未知，从 0 起算；首次内容尺寸事件会把它带到真正的底部。
		lastOffset.current = 0;
		wantBottom.current = true;
		setFarFromBottom(false);
		applyFollowing(true);
	}, [applyFollowing]);

	return { listRef, handlers, follow, reset, farFromBottom };
}
