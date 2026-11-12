import type { ControlEvent } from "@lework/api";
import { useEffect, useRef, useState } from "react";
import { AppState } from "react-native";
import EventSource from "react-native-sse";

import type { Settings } from "../settings/settings";
import { nextRetryDelay } from "./backoff";

// useControlEvents 维持一条到控制面的事件流。
//
// 移动端长期挂 SSE 会被系统杀掉，所以切后台就断开；回前台立即重连并让
// 调用方重拉快照，用快照纠正断线期间错过的增量。令牌只放在 header 里，
// 永远不进 URL。
export function useControlEvents(
	settings: Settings | null,
	onEvent: (event: ControlEvent) => void,
	onResync: () => void,
): { connected: boolean } {
	const [connected, setConnected] = useState(false);
	const onEventRef = useRef(onEvent);
	const onResyncRef = useRef(onResync);
	onEventRef.current = onEvent;
	onResyncRef.current = onResync;

	useEffect(() => {
		if (!settings) {
			setConnected(false);
			return;
		}

		let stopped = false;
		let source: EventSource | null = null;
		let timer: ReturnType<typeof setTimeout> | null = null;
		let attempt = 0;

		const closeSource = () => {
			source?.close();
			source = null;
		};

		const schedule = () => {
			if (stopped || timer) return;
			const delay = nextRetryDelay(attempt);
			attempt += 1;
			timer = setTimeout(() => {
				timer = null;
				connect();
			}, delay);
		};

		const connect = () => {
			if (stopped) return;
			closeSource();
			const next = new EventSource(`${settings.baseUrl}/api/events`, {
				headers: { Authorization: `Bearer ${settings.token}` },
			});
			source = next;

			next.addEventListener("open", () => {
				attempt = 0;
				setConnected(true);
			});
			next.addEventListener("message", (message) => {
				if (!message.data) return;
				try {
					onEventRef.current(JSON.parse(message.data) as ControlEvent);
				} catch {
					// 单条坏事件不应中断整条流；等下一次快照或事件纠正。
				}
			});
			next.addEventListener("error", () => {
				setConnected(false);
				schedule();
			});
			next.addEventListener("close", () => {
				setConnected(false);
				schedule();
			});
		};

		connect();

		const subscription = AppState.addEventListener("change", (state) => {
			if (state === "active") {
				attempt = 0;
				connect();
				onResyncRef.current();
			} else {
				closeSource();
				setConnected(false);
			}
		});

		return () => {
			stopped = true;
			subscription.remove();
			closeSource();
			if (timer) clearTimeout(timer);
			timer = null;
		};
	}, [settings?.baseUrl, settings?.token]);

	return { connected };
}
