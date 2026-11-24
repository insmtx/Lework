import { describe, expect, it } from "vitest";
import { affectsOverview, overviewStatesByNode, sessionStateClass } from "./session-state";

describe("affectsOverview", () => {
	it("识别会改变会话状态的几类事件", () => {
		for (const type of [
			"session.created",
			"session.updated",
			"session.execution.started",
			"permission.asked",
			"form.created",
			"agent.resync_required",
		]) {
			expect(affectsOverview(type)).toBe(true);
		}
	});

	it("高频消息事件不触发概览重拉", () => {
		expect(affectsOverview("message.created")).toBe(false);
		expect(affectsOverview("message.part.updated")).toBe(false);
	});
});

describe("overviewStatesByNode", () => {
	it("按节点与会话 ID 映射状态", () => {
		const states = overviewStatesByNode([
			{
				node_id: "n1",
				name: "n1",
				connected: true,
				sessions: [
					{
						session_id: "s1",
						state: "running",
						pending_permissions: 0,
						last_activity: "",
					},
					{
						session_id: "s2",
						state: "waiting_permission",
						pending_permissions: 1,
						last_activity: "",
					},
				],
			},
		]);
		expect(states.n1?.s1).toBe("running");
		expect(states.n1?.s2).toBe("waiting_permission");
	});
});

describe("sessionStateClass", () => {
	it("只给运行/等待/出错挂动画或底色", () => {
		expect(sessionStateClass("running")).toBe("lework-session-sweep-running");
		expect(sessionStateClass("waiting_permission")).toBe("lework-session-sweep-waiting");
		expect(sessionStateClass("error")).toBe("lework-session-sweep-error");
		expect(sessionStateClass("idle")).toBe("");
		expect(sessionStateClass("offline")).toBe("");
		expect(sessionStateClass(undefined)).toBe("");
	});
});
