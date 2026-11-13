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

	// 只依赖连接参数：settings 对象本身每次设置页保存都会换新，
	// 直接依赖它会让流在无关改动时重建。
	const baseUrl = settings?.baseUrl;
	const token = settings?.token;

	useEffect(() => {
		if (!baseUrl || !token) {
			setConnected(false);
			return;
		}

		let stopped = false;
		// foreground 控制重连：切后台必须彻底停下，否则系统会一直拉起
		// 连接，既费电又会在回前台时出现两条流。
		let foreground = AppState.currentState === "active";
		let source: EventSource | null = null;
		let timer: ReturnType<typeof setTimeout> | null = null;
		let attempt = 0;

		const clearTimer = () => {
			if (timer) clearTimeout(timer);
			timer = null;
		};

		const closeSource = () => {
			source?.close();
			source = null;
		};

		const schedule = () => {
			if (stopped || !foreground || timer) return;
			const delay = nextRetryDelay(attempt);
			attempt += 1;
			timer = setTimeout(() => {
				timer = null;
				connect();
			}, delay);
		};

		const connect = () => {
			if (stopped || !foreground) return;
			clearTimer();
			closeSource();
			const next = new EventSource<"resync">(`${baseUrl}/api/events`, {
				headers: { Authorization: `Bearer ${token}` },
			});
			source = next;

			next.addEventListener("open", () => {
				if (source !== next) return;
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
			// 服务端判定慢消费者时会发具名 resync 事件：增量已经断档，
			// 必须重新拉快照，不能继续在旧状态上叠加。
			next.addEventListener("resync", () => {
				if (source !== next) return;
				onResyncRef.current();
			});
			next.addEventListener("error", () => {
				// 旧连接的迟到事件不能污染新连接：source 已被替换时直接忽略。
				if (source !== next) return;
				setConnected(false);
				schedule();
			});
			next.addEventListener("close", () => {
				if (source !== next) return;
				setConnected(false);
				schedule();
			});
		};

		connect();

		const subscription = AppState.addEventListener("change", (state) => {
			const wasForeground = foreground;
			foreground = state === "active";
			if (foreground) {
				if (wasForeground) return;
				attempt = 0;
				connect();
				onResyncRef.current();
				return;
			}
			clearTimer();
			closeSource();
			setConnected(false);
		});

		return () => {
			stopped = true;
			subscription.remove();
			closeSource();
			clearTimer();
		};
	}, [baseUrl, token]);

	return { connected };
}
