import type { ControlEvent, Message } from "@lework/api";
import { describe, expect, it } from "vitest";

import { applyLiveEvent, clearBuffer, createLiveBuffer, mergeSnapshot } from "./live-buffer";

function event(type: string, data?: unknown): ControlEvent {
	return {
		node_id: "node_1",
		session_id: "ses_1",
		seq: 1,
		type,
		data,
		time: "2026-11-15T10:00:00Z",
	};
}

function delta(id: string, ordinal: number, text: string): ControlEvent {
	return event("session.text.delta", { assistantMessageID: id, ordinal, delta: text });
}

function ended(id: string, ordinal: number, text: string): ControlEvent {
	return event("session.text.ended", { assistantMessageID: id, ordinal, text });
}

function textOf(messages: Message[], id: string): string | undefined {
	return messages.find((message) => message.id === id)?.text;
}

describe("applyLiveEvent / mergeSnapshot", () => {
	it("delta 追加，ended 覆盖", () => {
		let buffer = createLiveBuffer();
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "你"));
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "好"));

		let merged = mergeSnapshot([], buffer);
		expect(merged.messages).toEqual([{ id: "msg_1", role: "assistant", text: "你好" }]);

		buffer = applyLiveEvent(buffer, ended("msg_1", 0, "你好！"));
		merged = mergeSnapshot([], buffer);
		expect(textOf(merged.messages, "msg_1")).toBe("你好！");
	});

	it("乱序 delta 按 ordinal 拼接，不产生重复文本", () => {
		let buffer = createLiveBuffer();
		buffer = applyLiveEvent(buffer, delta("msg_1", 1, "世界"));
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "你好，"));

		expect(textOf(mergeSnapshot([], buffer).messages, "msg_1")).toBe("你好，世界");
	});

	it("快照更长时采纳快照并丢弃缓冲", () => {
		const buffer = applyLiveEvent(createLiveBuffer(), delta("msg_1", 0, "你"));
		const snapshot: Message[] = [
			{
				id: "msg_1",
				role: "assistant",
				text: "你好，世界",
				parts: [{ type: "text", text: "你好，世界" }],
			},
		];

		const merged = mergeSnapshot(snapshot, buffer);

		expect(textOf(merged.messages, "msg_1")).toBe("你好，世界");
		expect(merged.buffer.parts.size).toBe(0);
	});

	it("快照更短时保留缓冲（不回缩）", () => {
		let buffer = createLiveBuffer();
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "你好"));
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "世界"));
		const snapshot: Message[] = [{ id: "msg_1", role: "assistant", text: "你好" }];

		const merged = mergeSnapshot(snapshot, buffer);

		expect(textOf(merged.messages, "msg_1")).toBe("你好世界");
		// 缓冲保留到下一次快照追上，晚到的 delta 仍能续上。
		expect(merged.buffer.parts.has("msg_1")).toBe(true);
	});

	it("快照里没有的消息追加到末尾", () => {
		const buffer = applyLiveEvent(createLiveBuffer(), delta("msg_2", 0, "新的回复"));
		const snapshot: Message[] = [{ id: "msg_1", role: "user", text: "发个任务" }];

		const merged = mergeSnapshot(snapshot, buffer);

		expect(merged.messages.map((message) => message.id)).toEqual(["msg_1", "msg_2"]);
		expect(merged.messages[1]).toEqual({ id: "msg_2", role: "assistant", text: "新的回复" });
	});

	it("无 parts 的助手消息只被文本替换，保留 tool parts", () => {
		const tool = { type: "tool", tool: "bash", callID: "call_1", state: { status: "running" } };
		const buffer = applyLiveEvent(createLiveBuffer(), ended("msg_1", 0, "新的完整文本"));
		const snapshot: Message[] = [{ id: "msg_1", role: "assistant", text: "旧", parts: [tool] }];

		const merged = mergeSnapshot(snapshot, buffer);
		const message = merged.messages.find((item) => item.id === "msg_1");

		expect(message?.text).toBe("新的完整文本");
		expect(message?.parts).toEqual([tool]);
		expect(message?.role).toBe("assistant");
	});

	it("reasoning.delta 不影响正文", () => {
		let buffer = applyLiveEvent(createLiveBuffer(), delta("msg_1", 0, "正文"));
		const before = buffer;
		buffer = applyLiveEvent(
			buffer,
			event("session.reasoning.delta", { assistantMessageID: "msg_1", ordinal: 0, delta: "思考" }),
		);

		expect(buffer).toBe(before);
		expect(textOf(mergeSnapshot([], buffer).messages, "msg_1")).toBe("正文");
	});

	it("无关事件返回原对象", () => {
		const buffer = createLiveBuffer();
		expect(applyLiveEvent(buffer, event("session.tool.called", { id: "tool_1" }))).toBe(buffer);
	});

	it("快照已输出到一半时，新 delta 继续追加而不是被丢弃", () => {
		// 打开子代理页时正文已经输出到「你好」，增量从订阅点之后才开始。
		const snapshot: Message[] = [
			{
				id: "msg_1",
				role: "assistant",
				text: "你好",
				parts: [{ type: "text", text: "你好" }],
			},
		];
		let buffer = createLiveBuffer();
		// 第一条 delta 与快照重叠，应该被快照吸收。
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "好"));
		expect(textOf(mergeSnapshot(snapshot, buffer).messages, "msg_1")).toBe("你好");

		// 之后的新正文必须立即可见，而不是等接口刷新。
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "世界"));
		const merged = mergeSnapshot(snapshot, buffer);

		expect(textOf(merged.messages, "msg_1")).toBe("你好世界");
	});

	it("快照刚好等于订阅点时从末尾继续", () => {
		const snapshot: Message[] = [
			{
				id: "msg_1",
				role: "assistant",
				text: "你好",
				parts: [{ type: "text", text: "你好" }],
			},
		];
		const buffer = applyLiveEvent(createLiveBuffer(), delta("msg_1", 0, "世界"));

		expect(textOf(mergeSnapshot(snapshot, buffer).messages, "msg_1")).toBe("你好世界");
	});

	it("快照领先时去掉重叠部分，不产生重复字符", () => {
		const snapshot: Message[] = [
			{
				id: "msg_1",
				role: "assistant",
				text: "你好世界",
				parts: [{ type: "text", text: "你好世界" }],
			},
		];
		let buffer = createLiveBuffer();
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "世界"));
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "你好"));

		expect(textOf(mergeSnapshot(snapshot, buffer).messages, "msg_1")).toBe("你好世界你好");
	});

	it("多段正文时按 text part 对齐，ended 覆盖同段旧前缀", () => {
		const tool = { type: "tool", tool: "bash", callID: "call_1", state: { status: "running" } };
		const snapshot: Message[] = [
			{
				id: "msg_1",
				role: "assistant",
				text: "你世界",
				parts: [{ type: "text", text: "你" }, tool, { type: "text", text: "世界" }],
			},
		];
		// 第一段正文已经结束，完整文本比快照里的前缀长；第二段仍是快照内容。
		const buffer = applyLiveEvent(createLiveBuffer(), ended("msg_1", 0, "你好"));

		const merged = mergeSnapshot(snapshot, buffer);
		const message = merged.messages.find((item) => item.id === "msg_1");

		expect(message?.text).toBe("你好世界");
		expect(message?.parts).toEqual([
			{ type: "text", text: "你" },
			tool,
			{ type: "text", text: "世界" },
		]);
	});

	it("ended 之后的迟到 delta 不会重复追加", () => {
		let buffer = applyLiveEvent(createLiveBuffer(), ended("msg_1", 0, "完整正文"));
		buffer = applyLiveEvent(buffer, delta("msg_1", 0, "完整正文"));

		expect(textOf(mergeSnapshot([], buffer).messages, "msg_1")).toBe("完整正文");
	});

	it("clearBuffer 清空缓冲", () => {
		const buffer = applyLiveEvent(createLiveBuffer(), delta("msg_1", 0, "你"));
		expect(mergeSnapshot([], clearBuffer()).messages).toEqual([]);
		expect(mergeSnapshot([], buffer).messages).toHaveLength(1);
	});
});
