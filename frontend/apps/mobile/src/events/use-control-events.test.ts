import { describe, expect, it, vi } from "vitest";

// 事件流 URL 是纯字符串逻辑，但模块本身依赖 react-native 的 AppState
// 与 EventSource。测试只关心拼接规则，用空桩替掉这两个运行时。
vi.mock("react-native", () => ({
	AppState: {
		currentState: "active",
		addEventListener: () => ({
			remove: () => {
				// 测试不解除监听。
			},
		}),
	},
}));
vi.mock("react-native-sse", () => ({ default: class {} }));

import { controlEventsURL } from "./use-control-events";

describe("controlEventsURL", () => {
	it("controlEventsURL encodes filters", () => {
		expect(controlEventsURL("http://cp", { nodeID: "n1", sessionID: "ses 1", live: true })).toBe(
			"http://cp/api/events?node=n1&session=ses%201&live=1",
		);
	});

	it("controlEventsURL keeps bare path without filters", () => {
		expect(controlEventsURL("http://cp", {})).toBe("http://cp/api/events");
		expect(controlEventsURL("http://cp")).toBe("http://cp/api/events");
	});

	it("只带部分过滤条件时不补空参数", () => {
		expect(controlEventsURL("http://cp", { live: true })).toBe("http://cp/api/events?live=1");
		expect(controlEventsURL("http://cp", { nodeID: "n1" })).toBe("http://cp/api/events?node=n1");
	});
});
