import { afterEach, describe, expect, it, vi } from "vitest";

import {
	notifySessionExpired,
	resetSessionExpiredHandlerForTests,
	setSessionExpiredHandler,
} from "./session-events";

afterEach(() => {
	resetSessionExpiredHandlerForTests();
});

describe("session-events", () => {
	it("注册后收到通知", () => {
		const handler = vi.fn();
		setSessionExpiredHandler(handler);
		notifySessionExpired();
		expect(handler).toHaveBeenCalledTimes(1);
	});

	it("取消注册后不再收到", () => {
		const handler = vi.fn();
		const off = setSessionExpiredHandler(handler);
		off();
		notifySessionExpired();
		expect(handler).not.toHaveBeenCalled();
	});

	it("没有注册者时通知是空操作", () => {
		expect(() => notifySessionExpired()).not.toThrow();
	});

	it("后注册的覆盖前一个，前一个的取消函数不影响新注册者", () => {
		const first = vi.fn();
		const second = vi.fn();
		const offFirst = setSessionExpiredHandler(first);
		setSessionExpiredHandler(second);
		offFirst();
		notifySessionExpired();
		expect(first).not.toHaveBeenCalled();
		expect(second).toHaveBeenCalledTimes(1);
	});
});
