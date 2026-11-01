"use client";

import { useEffect, useRef, useState } from "react";
import type { ControlEvent } from "./types";

interface UseControlEventsResult {
	connected: boolean;
}

const initialRetryDelay = 1_000;
const maxRetryDelay = 15_000;

/**
 * Opens the single process-wide SSE stream. When the connection drops, it
 * closes the native EventSource and reconnects with bounded exponential
 * backoff. The server sends a named `resync` event when a slow consumer was
 * dropped, so the page refetches snapshots instead of replaying events.
 */
export function useControlEvents(
	onEvent: (event: ControlEvent) => void,
	onResync: () => void,
): UseControlEventsResult {
	const eventRef = useRef(onEvent);
	const resyncRef = useRef(onResync);
	const [connected, setConnected] = useState(false);

	eventRef.current = onEvent;
	resyncRef.current = onResync;

	useEffect(() => {
		let closed = false;
		let source: EventSource | null = null;
		let retryTimer: ReturnType<typeof setTimeout> | null = null;
		let retryDelay = initialRetryDelay;

		const connect = () => {
			if (closed) return;
			source = new EventSource("/api/events");
			source.onopen = () => {
				retryDelay = initialRetryDelay;
				setConnected(true);
			};
			source.onerror = () => {
				setConnected(false);
				source?.close();
				source = null;
				if (closed) return;
				retryTimer = setTimeout(connect, retryDelay);
				retryDelay = Math.min(retryDelay * 2, maxRetryDelay);
			};
			source.onmessage = (message) => {
				try {
					eventRef.current(JSON.parse(message.data) as ControlEvent);
				} catch {
					// A malformed frame must not tear down the stream.
				}
			};
			source.addEventListener("resync", () => {
				resyncRef.current();
			});
		};

		connect();
		return () => {
			closed = true;
			if (retryTimer) clearTimeout(retryTimer);
			source?.close();
		};
	}, []);

	return { connected };
}
