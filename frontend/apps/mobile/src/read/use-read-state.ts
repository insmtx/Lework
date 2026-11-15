import * as SecureStore from "expo-secure-store";
import { useCallback, useEffect, useRef, useState } from "react";

import type { Settings } from "../settings/settings";
import {
	advance,
	parseWatermarks,
	pruneWatermarks,
	READ_KEY,
	serializeWatermarks,
	type Watermarks,
	watermarkKey,
} from "./read-state";

// ignoreWriteError 是故意的：SecureStore 写入失败（设备锁定、值超长）只影响
// 本轮之后的未读计算，不能让它冒泡到页面或者阻塞滚动。
function ignoreWriteError(): void {
	// 故意留空。
}

/**
 * useReadState 持有本机已读水位。
 *
 * 首次挂载读一次；markRead 先更新内存再 fire-and-forget 写回，失败静默。
 * 读取与写入之间可能交错（例如深链直达立刻贴底），所以用 ref 保存当前值，
 * 读取结果与内存取并集（只前进），不会被迟到的旧值覆盖。
 */
export function useReadState(
	settings: Settings | null,
	visibleKeys: readonly string[],
): {
	watermarks: Watermarks;
	markRead: (nodeID: string, sessionID: string, at?: number) => void;
} {
	const [watermarks, setWatermarks] = useState<Watermarks>({});
	const current = useRef<Watermarks>({});
	const visible = useRef<readonly string[]>(visibleKeys);
	visible.current = visibleKeys;

	// 换控制面（配对另一台机器）时重新读一次；没有连接就不必碰 keystore。
	const baseUrl = settings?.baseUrl ?? "";

	useEffect(() => {
		if (!baseUrl) return;
		let cancelled = false;
		void SecureStore.getItemAsync(READ_KEY)
			.then((raw) => {
				if (cancelled) return;
				let next = current.current;
				for (const [key, at] of Object.entries(parseWatermarks(raw))) {
					next = advance(next, key, at);
				}
				current.current = next;
				setWatermarks(next);
			})
			.catch(() => {
				// 读不到历史水位就当没有：本轮最多少显示几个未读点。
			});
		return () => {
			cancelled = true;
		};
	}, [baseUrl]);

	const markRead = useCallback((nodeID: string, sessionID: string, at: number = Date.now()) => {
		const next = advance(current.current, watermarkKey(nodeID, sessionID), at);
		if (next === current.current) return;
		current.current = next;
		setWatermarks(next);
		// 只在写回时剪枝：visibleKeys 是当前列表里的会话，优先保留它们。
		const pruned = pruneWatermarks(next, visible.current);
		void SecureStore.setItemAsync(READ_KEY, serializeWatermarks(pruned)).catch(ignoreWriteError);
	}, []);

	return { watermarks, markRead };
}
