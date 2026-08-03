"use client";

import { trackButtonClick, trackPageStay, trackPageView } from "@leros/store";
import { useEffect } from "react";

const PAGE_STAY_THRESHOLD_MS = 15_000;
const TRACKED_BUTTON_SELECTOR = 'button, [role="button"], [data-slot="button"]';

export function FrontendEventTracker({ currentPath }: { currentPath?: string }) {
	useEffect(() => {
		// 中文注释：延迟到当前 effect 周期结束，避免 React StrictMode 的开发态重复挂载上报两次 PV。
		const pageViewTimer = setTimeout(() => trackPageView(), 0);

		let visibleDurationMs = 0;
		let visibleSince = document.visibilityState === "visible" ? Date.now() : null;
		let reported = false;
		let timer: ReturnType<typeof setTimeout> | undefined;

		const clearTimer = () => {
			if (timer !== undefined) {
				clearTimeout(timer);
				timer = undefined;
			}
		};

		const scheduleReport = () => {
			clearTimer();
			if (reported || visibleSince === null) return;

			const remainingMs = Math.max(PAGE_STAY_THRESHOLD_MS - visibleDurationMs, 0);
			timer = setTimeout(() => {
				if (reported || visibleSince === null) return;
				const durationMs = visibleDurationMs + (Date.now() - visibleSince);
				if (durationMs < PAGE_STAY_THRESHOLD_MS) {
					visibleDurationMs = durationMs;
					visibleSince = Date.now();
					scheduleReport();
					return;
				}

				reported = true;
				trackPageStay(durationMs);
				clearTimer();
			}, remainingMs);
		};

		const handleVisibilityChange = () => {
			const now = Date.now();
			if (document.visibilityState === "hidden") {
				if (visibleSince !== null) {
					visibleDurationMs += now - visibleSince;
					visibleSince = null;
				}
				clearTimer();
				return;
			}

			if (visibleSince === null) {
				visibleSince = now;
			}
			scheduleReport();
		};

		document.addEventListener("visibilitychange", handleVisibilityChange);
		scheduleReport();

		return () => {
			clearTimeout(pageViewTimer);
			clearTimer();
			document.removeEventListener("visibilitychange", handleVisibilityChange);
		};
	}, [currentPath]);

	useEffect(() => {
		const handleClick = (event: MouseEvent) => {
			if (!(event.target instanceof Element)) return;

			const button = event.target.closest<HTMLElement>(TRACKED_BUTTON_SELECTOR);
			if (
				!button ||
				button.matches(":disabled") ||
				button.getAttribute("aria-disabled") === "true"
			) {
				return;
			}

			const eventName =
				button.dataset.trackName?.trim() ||
				button.getAttribute("aria-label")?.trim() ||
				button.textContent?.replace(/\s+/g, " ").trim() ||
				button.getAttribute("title")?.trim() ||
				button.getAttribute("name")?.trim() ||
				button.id ||
				"button";

			trackButtonClick(eventName, event.clientX, event.clientY);
		};

		document.addEventListener("click", handleClick);
		return () => document.removeEventListener("click", handleClick);
	}, []);

	return null;
}
