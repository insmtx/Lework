import type { Session } from "@lework/api";
import { describe, expect, it } from "vitest";

import { pickSubagentSession } from "./pick-subagent-session";

function child(overrides: Partial<Session>): Session {
	return { id: "ses_child", ...overrides };
}

describe("pickSubagentSession", () => {
	it("标题与 description 完全一致时选中该子会话", () => {
		const children = [
			child({ id: "ses_a", title: "Review Task 16", updatedAt: "2026-11-15T10:00:00Z" }),
			child({ id: "ses_b", title: "Implement Task 16", updatedAt: "2026-11-15T09:00:00Z" }),
		];
		expect(pickSubagentSession(children, { description: "Review Task 16" })?.id).toBe("ses_a");
	});

	it("多条标题命中时取最新更新的一条", () => {
		const children = [
			child({ id: "ses_old", title: "Review Task 16", updatedAt: "2026-11-15T09:00:00Z" }),
			child({ id: "ses_new", title: "Review Task 16", updatedAt: "2026-11-15T11:00:00Z" }),
		];
		expect(pickSubagentSession(children, { description: "Review Task 16" })?.id).toBe("ses_new");
	});

	it("同标题有多个时优先仍在执行的子会话", () => {
		const children = [
			child({ id: "ses_newer_idle", title: "Review Task 16", updatedAt: "2026-11-15T11:00:00Z" }),
			child({
				id: "ses_running",
				title: "Review Task 16",
				updatedAt: "2026-11-15T09:00:00Z",
				running: true,
			}),
		];
		expect(
			pickSubagentSession(children, { description: "Review Task 16", running: true })?.id,
		).toBe("ses_running");
	});

	it("标题对不上时用唯一 running 的子会话（修复轮次复用旧标题）", () => {
		const children = [
			child({ id: "ses_done", title: "Implement Task 18", updatedAt: "2026-11-15T11:00:00Z" }),
			child({
				id: "ses_fixing",
				title: "Implement Task 18",
				updatedAt: "2026-11-15T10:00:00Z",
				running: true,
			}),
		];
		expect(
			pickSubagentSession(children, { description: "Fix round 1: Task 18 findings", running: true })
				?.id,
		).toBe("ses_fixing");
	});

	it("多个 running 子会话时取最近更新的一个", () => {
		const children = [
			child({ id: "ses_old", running: true, updatedAt: "2026-11-15T09:00:00Z" }),
			child({ id: "ses_new", running: true, updatedAt: "2026-11-15T10:00:00Z" }),
		];
		expect(
			pickSubagentSession(children, { description: "Review Task 16", running: true })?.id,
		).toBe("ses_new");
	});

	it("标题对不上但只有一个子会话时用它", () => {
		const children = [child({ id: "ses_only", title: "Implement Task 16" })];
		expect(pickSubagentSession(children, { description: "Review Task 16" })?.id).toBe("ses_only");
	});

	it("标题对不上且有多个子会话时拒绝猜测", () => {
		const children = [
			child({ id: "ses_a", title: "Implement Task 16" }),
			child({ id: "ses_b", title: "Review Task 15" }),
		];
		expect(pickSubagentSession(children, { description: "Review Task 16" })).toBeNull();
	});

	it("执行中但没有 running 子会话时继续等待，不拿历史会话凑数", () => {
		const children = [
			child({ id: "ses_a", title: "Implement Task 16" }),
			child({ id: "ses_b", title: "Review Task 15" }),
		];
		expect(
			pickSubagentSession(children, { description: "Review Task 16", running: true }),
		).toBeNull();
	});

	it("没有 description 时只在唯一子会话时选中", () => {
		expect(pickSubagentSession([child({ id: "ses_only" })], { description: "" })?.id).toBe(
			"ses_only",
		);
		expect(
			pickSubagentSession([child({ id: "ses_a" }), child({ id: "ses_b" })], { description: "" }),
		).toBeNull();
	});

	it("没有子会话时返回 null", () => {
		expect(pickSubagentSession([], { description: "Review Task 16" })).toBeNull();
	});

	it("忽略标题空白差异", () => {
		const children = [child({ id: "ses_a", title: "  Review Task 16  " })];
		expect(pickSubagentSession(children, { description: "Review Task 16" })?.id).toBe("ses_a");
	});
});
