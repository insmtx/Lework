import { useCallback, useRef } from "react";
import type {
	FlatList,
	LayoutChangeEvent,
	NativeScrollEvent,
	NativeSyntheticEvent,
} from "react-native";

import { bottomOffset, followOffset, isNearBottom } from "./scroll";

/** 超过这段时间没有新的滚动事件，就认为上一次手指拖动已经结束。 */
const GESTURE_IDLE_MS = 300;

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
 * 不用 FlatList.scrollToEnd：它在没有 getItemLayout 的变高列表里按已测量行
 * 的平均高度估算末尾（RN 源码注明这样会 janky），而滚动本身又会改变测量窗口，
 * 流式期间会形成「滚动 → 重新测量 → 内容尺寸变化 → 再滚动」的自激循环，表现
 * 为上下乱跳。这里改为用 onContentSizeChange 给的真实高度贴底。
 *
 * 跟随状态只看每一帧的滚动位置是否贴底，不依赖「拖动结束/惯性结束」事件的
 * 完整性：native 在「按住正在惯性的列表再松手」或「点一下让它停住」时可能
 * 不发 end 事件，用标志位判断会让跟随永久失效。拖动标志只用来避免在手指
 * 按住时抢滚动，并用静默时长兜底。
 *
 * 贴底只向底部追：内容高度因虚拟列表重新测量而变小时不主动回滚，交给系统
 * 把偏移夹回新的底部，避免和流式增长对拉出抖动。
 */
export function useAutoFollow<T>({
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
	const listRef = useRef<FlatList<T>>(null);
	const contentHeight = useRef(0);
	const viewportHeight = useRef(0);
	// 同一帧内可能连续收到内容尺寸与滚动事件，同步镜像保证读到最新意图。
	const followingRef = useRef(following);
	followingRef.current = following;
	// 手指是否按在列表上；只用来避免用户拖动时抢滚动。
	const dragging = useRef(false);
	const lastScrollAt = useRef(0);
	// 最近一次已知的滚动位置（含程序化滚动），用来判断是否还需要往底部追。
	const lastOffset = useRef(0);

	// native 可能漏发 onScrollEndDrag，用「一段时间没有滚动事件」兜底解除拖动状态。
	const releaseStaleDrag = useCallback(() => {
		if (!dragging.current) return;
		if (Date.now() - lastScrollAt.current < GESTURE_IDLE_MS) return;
		dragging.current = false;
	}, []);

	const applyScroll = useCallback(() => {
		releaseStaleDrag();
		if (!followingRef.current || dragging.current) return;
		if (viewportHeight.current <= 0 || contentHeight.current <= 0) return;
		const target = bottomOffset(contentHeight.current, viewportHeight.current);
		const next = followOffset(target, lastOffset.current);
		if (next === null) return;
		// 自动贴底不做动画：流式增量会让动画不断被重启，而且动画的中间帧
		// 会被误当成用户滚动。瞬时落到底反而更稳。
		listRef.current?.scrollToOffset({ offset: next, animated: false });
	}, [releaseStaleDrag]);

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
			const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
			lastOffset.current = contentOffset.y;
			lastScrollAt.current = Date.now();
			// 程序化贴底永远落在底部，所以不必区分手势来源：只看这一帧是否贴底，
			// 即使 native 漏发 end 事件，跟随状态也不会失真。
			const next = isNearBottom({
				offsetY: contentOffset.y,
				contentHeight: contentSize.height,
				viewportHeight: layoutMeasurement.height,
			});
			applyFollowing(next);
			if (next) onAtBottom?.();
		},
		[applyFollowing, onAtBottom],
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
			lastScrollAt.current = Date.now();
		},
		onScrollEndDrag: () => {
			dragging.current = false;
			// 拖动期间内容可能已经变长；松手时仍贴底就补一次对齐。
			applyScroll();
		},
		onMomentumScrollBegin: () => {
			// 惯性开始意味着拖动已经结束，只保留最终的贴底对齐。
			dragging.current = false;
		},
		onMomentumScrollEnd: () => {
			applyScroll();
		},
	};

	/** follow 恢复跟随并立即贴底。 */
	const follow = useCallback(() => {
		applyFollowing(true);
		applyScroll();
	}, [applyFollowing, applyScroll]);

	/** reset 在切换会话时调用：恢复跟随，等新内容测量完成后由尺寸事件贴底。 */
	const reset = useCallback(() => {
		dragging.current = false;
		lastScrollAt.current = 0;
		// 新会话的滚动位置未知，从 0 起算；首帧尺寸事件会把它带到真正的底部。
		lastOffset.current = 0;
		applyFollowing(true);
	}, [applyFollowing]);

	return { listRef, handlers, follow, reset };
}
