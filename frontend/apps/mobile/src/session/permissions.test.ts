import { describe, expect, it } from "vitest";
import type { ControlEvent, PendingPermission } from "@lework/api";
import { mergePermission } from "./permissions";

const asked: ControlEvent = {
	node_id: "node_1",
	session_id: "ses_1",
	seq: 1,
	type: "permission.asked",
	data: { id: "perm_1", action: "bash", resources: ["rm -rf build"], message: "run build" },
	time: "2026-11-12T00:00:00Z",
};

describe("mergePermission", () => {
	it("首次 asked 追加一张卡", () => {
		const list = mergePermission([], asked);
		expect(list).toEqual([
			{ id: "perm_1", session_id: "ses_1", action: "bash", resources: ["rm -rf build"], message: "run build" },
		]);
	});

	it("重复 asked 不重复追加", () => {
		const once = mergePermission([], asked);
		expect(mergePermission(once, asked)).toEqual(once);
	});

	it("replied / resolved 按 ID 删除", () => {
		const list = mergePermission([], asked);
		const replied = { ...asked, type: "permission.replied" };
		expect(mergePermission(list, replied)).toEqual([]);
		const resolved = { ...asked, type: "permission.resolved" };
		expect(mergePermission(list, resolved)).toEqual([]);
	});

	it("其他事件不改变列表", () => {
		const list = mergePermission([], asked);
		expect(mergePermission(list, { ...asked, type: "message.part.updated" })).toEqual(list);
	});

	it("负载缺失时不构造空卡", () => {
		expect(mergePermission([], { ...asked, data: { action: "bash" } })).toEqual([]);
	});

	it("不修改输入数组", () => {
		const list: PendingPermission[] = [];
		const next = mergePermission(list, asked);
		expect(list).toEqual([]);
		expect(next).toHaveLength(1);
	});
});
