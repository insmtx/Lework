import type { ControlEvent, Message } from "@lework/api";

import { messageText } from "../session/messages";

/**
 * LiveBuffer 是按 assistantMessageID 归集的流式文本。
 *
 * 每个 ordinal 对应 OpenCode 的一个 text part；分开存是为了按序号拼出正文，
 * 乱序到达的 delta 不会互相覆盖出重复文本。结构不可变：每次 apply 返回新
 * 对象，React 才能靠引用变化判断是否需要重绘。
 */
export interface LiveBuffer {
	readonly parts: ReadonlyMap<string, ReadonlyMap<number, string>>;
}

/** createLiveBuffer 返回空缓冲。 */
export function createLiveBuffer(): LiveBuffer {
	return { parts: new Map() };
}

/** clearBuffer 丢弃全部增量；重连或 resync 后必须从快照重建。 */
export function clearBuffer(): LiveBuffer {
	return createLiveBuffer();
}

interface TextChunk {
	id: string;
	ordinal: number;
	chunk: string;
}

// readChunk 从事件负载里取文本增量。字段缺失或类型不对时返回 null：
// 单条坏事件不应让整段流式文本错位。
function readChunk(event: ControlEvent, field: "delta" | "text"): TextChunk | null {
	if (!event.data || typeof event.data !== "object") return null;
	const data = event.data as {
		assistantMessageID?: unknown;
		ordinal?: unknown;
		delta?: unknown;
		text?: unknown;
	};
	if (typeof data.assistantMessageID !== "string" || data.assistantMessageID === "") return null;
	if (typeof data.ordinal !== "number" || !Number.isFinite(data.ordinal)) return null;
	const chunk = data[field];
	if (typeof chunk !== "string") return null;
	return { id: data.assistantMessageID, ordinal: data.ordinal, chunk };
}

function writePart(
	buffer: LiveBuffer,
	id: string,
	ordinal: number,
	write: (previous: string) => string,
): LiveBuffer {
	const parts = new Map(buffer.parts.get(id) ?? []);
	parts.set(ordinal, write(parts.get(ordinal) ?? ""));
	const next = new Map(buffer.parts);
	next.set(id, parts);
	return { parts: next };
}

/**
 * applyLiveEvent 把一条增量并入缓冲。
 *
 * 只处理正文相关事件：delta 追加，ended 用完整文本覆盖该 ordinal（权威）；
 * reasoning 属于执行过程，不进正文。其它事件返回原对象，调用方可以据此
 * 跳过重绘。
 */
export function applyLiveEvent(buffer: LiveBuffer, event: ControlEvent): LiveBuffer {
	switch (event.type) {
		case "session.text.delta": {
			const chunk = readChunk(event, "delta");
			if (!chunk) return buffer;
			return writePart(buffer, chunk.id, chunk.ordinal, (previous) => previous + chunk.chunk);
		}
		case "session.text.ended": {
			const chunk = readChunk(event, "text");
			if (!chunk) return buffer;
			return writePart(buffer, chunk.id, chunk.ordinal, () => chunk.chunk);
		}
		default:
			return buffer;
	}
}

/** bufferText 按 ordinal 升序拼接某个助手消息的正文。 */
function bufferText(parts: ReadonlyMap<number, string>): string {
	return [...parts.entries()]
		.sort((left, right) => left[0] - right[0])
		.map(([, text]) => text)
		.join("");
}

/**
 * mergeSnapshot 把服务端快照与本地缓冲对齐。
 *
 * 关键规则：快照只允许延伸、不允许缩短。快照文本不短于缓冲时说明它已经是
 * 更完整的前缀，采纳并丢弃缓冲；快照更短说明请求发出时流还没写完，必须
 * 保留缓冲，否则流式文本会突然回缩。快照里没有的缓冲消息追加到末尾，
 * 让增量领先于快照时也能立刻看到内容。
 */
export function mergeSnapshot(
	snapshot: Message[],
	buffer: LiveBuffer,
): { messages: Message[]; buffer: LiveBuffer } {
	const remaining = new Map(buffer.parts);
	const messages = snapshot.map((message) => {
		const parts = remaining.get(message.id);
		if (!parts) return message;
		const live = bufferText(parts);
		if (live.length > messageText(message).length) {
			// 只替换 text，保留原 parts：工具卡片来自快照，不能被增量抹掉。
			return { ...message, text: live };
		}
		remaining.delete(message.id);
		return message;
	});

	for (const [id, parts] of remaining) {
		const text = bufferText(parts);
		if (text === "") continue;
		messages.push({ id, role: "assistant", text });
	}

	return { messages, buffer: { parts: remaining } };
}
