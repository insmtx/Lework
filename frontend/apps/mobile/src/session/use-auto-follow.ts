import { useCallback, useRef } from "react";
import type {
	FlatList,
	LayoutChangeEvent,
	NativeScrollEvent,
	NativeSyntheticEvent,
} from "react-native";

import { bottomOffset, isNearBottom } from "./scroll";

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
 * 为上下乱跳。这里改为用 onContentSizeChange 给的真实高度贴底，并且只把手指
 * 产生的滚动（拖动与其惯性）当作切换跟随的依据，程序化贴底产生的 scroll 事件
 * 不参与判断，避免某一帧把跟随误关。
 */
export function useAutoFollow<T>({
	following,
	setFollowing,
	onAtBottom,
}: {
	/** 父级持有的跟随状态；它同时驱动「有新内容」提示。 */
	following: boolean;
	setFollowing: (following: boolean) => void;
	/** 用户自己滚回底部时回调；程序化贴底不会触发。 */
	onAtBottom?: () => void;
}) {
	const listRef = useRef<FlatList<T>>(null);
	const contentHeight = useRef(0);
	const viewportHeight = useRef(0);
	// 同一帧内可能连续收到内容尺寸与滚动事件，同步镜像保证读到最新意图。
	const followingRef = useRef(following);
	followingRef.current = following;
	const dragging = useRef(false);
	const momentum = useRef(false);

	const scrollToBottom = useCallback(() => {
		if (viewportHeight.current <= 0 || contentHeight.current <= 0) return;
		// 自动与主动贴底都不做动画：流式增量会让动画不断被重启，而且动画的
		// 中间帧会被误当成用户滚动。瞬时落到底反而更稳。
		listRef.current?.scrollToOffset({
			offset: bottomOffset(contentHeight.current, viewportHeight.current),
			animated: false,
		});
	}, []);

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
			// 程序化滚动的中间帧不能把跟随关掉，否则流式内容还在长就停了。
			if (!dragging.current && !momentum.current) return;
			const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
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
			if (!followingRef.current || dragging.current || momentum.current) return;
			scrollToBottom();
		},
		onLayout: (event) => {
			viewportHeight.current = event.nativeEvent.layout.height;
			// 键盘、旋转或输入框长高后重新贴底，但不打断用户正在进行的滚动。
			if (!followingRef.current || dragging.current || momentum.current) return;
			scrollToBottom();
		},
		onScroll: handleScroll,
		onScrollBeginDrag: () => {
			dragging.current = true;
		},
		onScrollEndDrag: () => {
			dragging.current = false;
		},
		onMomentumScrollBegin: () => {
			momentum.current = true;
		},
		onMomentumScrollEnd: () => {
			momentum.current = false;
			// 惯性结束时如果仍贴底，补一次对齐；上翻看历史则保持原位。
			if (followingRef.current) scrollToBottom();
		},
	};

	/** follow 恢复跟随并立即贴底。 */
	const follow = useCallback(() => {
		applyFollowing(true);
		scrollToBottom();
	}, [applyFollowing, scrollToBottom]);

	/** reset 在切换会话时调用：恢复跟随，等新内容测量完成后由尺寸事件贴底。 */
	const reset = useCallback(() => {
		dragging.current = false;
		momentum.current = false;
		applyFollowing(true);
	}, [applyFollowing]);

	return { listRef, handlers, follow, reset };
}
