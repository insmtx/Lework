import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	trackButtonClick: vi.fn(),
	trackPageStay: vi.fn(),
	trackPageView: vi.fn(),
}));

vi.mock("@leros/store", () => storeMocks);

import { FrontendEventTracker } from "./FrontendEventTracker";

describe("FrontendEventTracker", () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-07-24T00:00:00Z"));
		Object.defineProperty(document, "visibilityState", {
			configurable: true,
			value: "visible",
		});
		storeMocks.trackButtonClick.mockReset();
		storeMocks.trackPageStay.mockReset();
		storeMocks.trackPageView.mockReset();
	});

	afterEach(() => {
		cleanup();
		vi.useRealTimers();
	});

	it("reports one page view on mount and route changes in StrictMode", () => {
		const { rerender } = render(
			<StrictMode>
				<FrontendEventTracker currentPath="/workbench" />
			</StrictMode>,
		);

		vi.advanceTimersByTime(0);
		expect(storeMocks.trackPageView).toHaveBeenCalledTimes(1);
		rerender(
			<StrictMode>
				<FrontendEventTracker currentPath="/projects" />
			</StrictMode>,
		);
		vi.advanceTimersByTime(0);
		expect(storeMocks.trackPageView).toHaveBeenCalledTimes(2);
	});

	it("reports one stay event after fifteen visible seconds", () => {
		render(<FrontendEventTracker currentPath="/workbench" />);

		vi.advanceTimersByTime(14_999);
		expect(storeMocks.trackPageStay).not.toHaveBeenCalled();

		vi.advanceTimersByTime(1);
		expect(storeMocks.trackPageStay).toHaveBeenCalledTimes(1);
		expect(storeMocks.trackPageStay).toHaveBeenCalledWith(15_000);

		vi.advanceTimersByTime(15_000);
		expect(storeMocks.trackPageStay).toHaveBeenCalledTimes(1);
	});

	it("reports delegated button clicks with the explicit tracking name", () => {
		render(
			<>
				<FrontendEventTracker currentPath="/workbench" />
				<button type="button" data-track-name="create-project">
					新建项目
				</button>
			</>,
		);

		fireEvent.click(screen.getByRole("button"), { clientX: 12, clientY: 34 });

		expect(storeMocks.trackButtonClick).toHaveBeenCalledWith("create-project", 12, 34);
	});

	it("cleans up timers and click listeners when unmounted", () => {
		const button = document.createElement("button");
		button.textContent = "卸载后按钮";
		document.body.appendChild(button);
		const { unmount } = render(<FrontendEventTracker currentPath="/workbench" />);

		unmount();
		vi.advanceTimersByTime(15_000);
		fireEvent.click(button);

		expect(storeMocks.trackPageStay).not.toHaveBeenCalled();
		expect(storeMocks.trackButtonClick).not.toHaveBeenCalled();
		button.remove();
	});
});
