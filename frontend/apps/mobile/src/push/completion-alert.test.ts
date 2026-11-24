import type { ControlEvent } from "@lework/api";
import { describe, expect, it } from "vitest";

import { completionAlertFrom } from "./completion-alert";

function event(type: string, session: string | undefined = "ses_1"): ControlEvent {
	return {
		node_id: "node_1",
		session_id: session,
		seq: 1,
		type,
		time: "2026-11-25T01:00:00Z",
	};
}

describe("completionAlertFrom", () => {
	it("执行成功生成完成提醒", () => {
		expect(completionAlertFrom(event("session.execution.succeeded"))).toEqual({
			nodeID: "node_1",
			sessionID: "ses_1",
			kind: "done",
			title: "执行完成",
			body: "点开查看结果",
		});
	});

	it("执行失败生成失败提醒", () => {
		expect(completionAlertFrom(event("session.execution.failed"))).toMatchObject({
			kind: "failed",
			title: "执行失败",
		});
	});

	// interrupted 和审批事件都不是「跑完了」，本地不响铃。
	it("中断与其它事件不提醒", () => {
		expect(completionAlertFrom(event("session.execution.interrupted"))).toBeNull();
		expect(completionAlertFrom(event("permission.asked"))).toBeNull();
	});

	it("没有会话 id 的事件不提醒", () => {
		expect(completionAlertFrom(event("session.execution.succeeded", ""))).toBeNull();
		expect(
			completionAlertFrom({
				node_id: "node_1",
				seq: 1,
				type: "session.execution.succeeded",
				time: "2026-11-25T01:00:00Z",
			}),
		).toBeNull();
	});
});
