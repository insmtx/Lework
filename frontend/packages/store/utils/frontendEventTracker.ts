import {
	type FrontendEvent,
	type FrontendEventExtra,
	frontendEventApi,
} from "../api/frontendEventApi";

const FRONTEND_EVENT_FINGERPRINT_KEY = "leros-frontend-event-fingerprint";

let memoryFingerprint = "";

function createFingerprint(): string {
	if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
		return crypto.randomUUID();
	}

	if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
		const bytes = crypto.getRandomValues(new Uint8Array(16));
		bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
		bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
		const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
		return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
	}

	return `leros-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function getFrontendEventFingerprint(): string {
	if (memoryFingerprint) return memoryFingerprint;

	if (typeof window !== "undefined") {
		try {
			const storedFingerprint = window.localStorage.getItem(FRONTEND_EVENT_FINGERPRINT_KEY);
			if (storedFingerprint) {
				memoryFingerprint = storedFingerprint;
				return memoryFingerprint;
			}
		} catch {
			// 中文注释：隐私模式可能禁用 localStorage，后续使用当前会话内的内存指纹。
		}
	}

	memoryFingerprint = createFingerprint();
	if (typeof window !== "undefined") {
		try {
			window.localStorage.setItem(FRONTEND_EVENT_FINGERPRINT_KEY, memoryFingerprint);
		} catch {
			// 中文注释：持久化失败不应阻断业务或事件上报。
		}
	}
	return memoryFingerprint;
}

function withPageContext(
	event: Omit<FrontendEvent, "timestamp"> & Partial<Pick<FrontendEvent, "timestamp">>,
): FrontendEvent {
	const viewport: FrontendEventExtra =
		typeof window === "undefined"
			? {}
			: {
					window_width: window.innerWidth,
					window_height: window.innerHeight,
				};

	return {
		...event,
		timestamp: event.timestamp ?? Date.now(),
		page_url: event.page_url ?? (typeof window === "undefined" ? "" : window.location.href),
		page_title: event.page_title ?? (typeof document === "undefined" ? "" : document.title),
		extra: {
			...viewport,
			...event.extra,
		},
	};
}

export function trackFrontendEvent(
	event: Omit<FrontendEvent, "timestamp"> & Partial<Pick<FrontendEvent, "timestamp">>,
): void {
	if (typeof window === "undefined" || !event.event_type) return;

	void frontendEventApi
		.collect({
			fingerprint: getFrontendEventFingerprint(),
			events: [withPageContext(event)],
		})
		.catch(() => {
			// 中文注释：行为统计失败必须静默，不能干扰用户操作或形成递归错误。
		});
}

export function trackPageView(): void {
	trackFrontendEvent({ event_type: "page_view" });
}

export function trackPageStay(durationMs: number): void {
	trackFrontendEvent({
		event_type: "page_stay_15s",
		duration_ms: durationMs,
	});
}

export function trackButtonClick(eventName: string, clickX: number, clickY: number): void {
	trackFrontendEvent({
		event_type: "button_click",
		event_name: eventName,
		extra: {
			click_x: clickX,
			click_y: clickY,
		},
	});
}
