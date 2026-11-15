import type { ControlEvent, Message } from "@lework/api";

import { messageText } from "../session/messages";

/**
 * LivePart 是某个 text part 自订阅以来收到的内容。
 *
 * ended 为 true 时 text 是 OpenCode 给的完整正文，而不是从订阅点开始的尾巴；
 * 合并时必须按权威文本处理，否则会把整段回复拼到旧快照后面。
 */
interface LivePart {
	readonly text: string;
	readonly ended: boolean;
}

/**
 * LiveBuffer 是按 assistantMessageID 归集的流式文本。
 *
 * 每个 ordinal 对应 OpenCode 的一个 text part；分开存是为了按序号拼出正文，
 * 乱序到达的 delta 不会互相覆盖出重复文本。结构不可变：每次 apply 返回新
 * 对象，React 才能靠引用变化判断是否需要重绘。
 */
export interface LiveBuffer {
	readonly parts: ReadonlyMap<string, ReadonlyMap<number, LivePart>>;
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
	write: (previous: LivePart) => LivePart,
): LiveBuffer {
	const parts = new Map(buffer.parts.get(id) ?? []);
	parts.set(ordinal, write(parts.get(ordinal) ?? { text: "", ended: false }));
	const next = new Map(buffer.parts);
	next.set(id, parts);
	return { parts: next };
}

/**
 * applyLiveEvent 把一条增量并入缓冲。
 *
 * 只处理正文相关事件：delta 追加，ended 用完整文本覆盖该 ordinal（权威）；
 * reasoning 属于执行过程，不进正文。其它事件返回原对象，调用方可以据此
 * 跳过重绘。ended 之后同 ordinal 的迟到 delta 直接丢弃，避免把完整文本
 * 又追加一遍。
 */
export function applyLiveEvent(buffer: LiveBuffer, event: ControlEvent): LiveBuffer {
	switch (event.type) {
		case "session.text.delta": {
			const chunk = readChunk(event, "delta");
			if (!chunk) return buffer;
			return writePart(buffer, chunk.id, chunk.ordinal, (previous) =>
				previous.ended ? previous : { text: previous.text + chunk.chunk, ended: false },
			);
		}
		case "session.text.ended": {
			const chunk = readChunk(event, "text");
			if (!chunk) return buffer;
			return writePart(buffer, chunk.id, chunk.ordinal, () => ({
				text: chunk.chunk,
				ended: true,
			}));
		}
		default:
			return buffer;
	}
}

/** bufferText 按 ordinal 升序拼接某个助手消息的正文。 */
function bufferText(parts: ReadonlyMap<number, LivePart>): string {
	return [...parts.entries()]
		.sort((left, right) => left[0] - right[0])
		.map(([, part]) => part.text)
		.join("");
}

/**
 * mergeLiveText 把快照正文与订阅以来的增量对齐。
 *
 * 快照是「从开头到某个时间点」的前缀，增量是「从订阅点到现在」的尾巴；
 * 两者可能重叠，也可能增量完全被快照包含。最简单的做法是查找最长的
 * 「快照后缀 == 增量前缀」，重叠部分只保留一份：
 *
 * - 快照已包含增量 → 采纳快照（订阅后先拉快照时很常见）；
 * - 增量从快照中间开始 → 快照 + 增量未覆盖的尾部；
 * - 没有重叠 → 增量是快照之后的新正文，直接追加。
 *
 * 旧实现用「谁更长」判断，半途打开会话时快照通常更长，于是把缓冲删掉，
 * 后续 delta 全部丢失，只能等接口刷新——这就是「流式不及时」的根因。
 */
function mergeLiveText(base: string, live: string): string {
	if (live === "") return base;
	if (base === "") return live;
	if (base.includes(live)) return base;

	// 重叠部分最多等于增量长度；从最小起点开始找，命中的就是最长重叠。
	const first = live.charCodeAt(0);
	const minStart = Math.max(0, base.length - live.length);
	for (let start = minStart; start < base.length; start++) {
		if (base.charCodeAt(start) !== first) continue;
		const overlap = base.length - start;
		let matched = true;
		for (let index = 1; index < overlap; index++) {
			if (base.charCodeAt(start + index) !== live.charCodeAt(index)) {
				matched = false;
				break;
			}
		}
		if (matched) return base + live.slice(overlap);
	}
	return base + live;
}

/** snapshotTextParts 按正文顺序返回快照里的 text part，ordinal 与它一一对应。 */
function snapshotTextParts(message: Message): string[] {
	return (message.parts ?? [])
		.filter((part) => part.type === "text")
		.map((part) => part.text ?? "");
}

/**
 * mergeSnapshot 把服务端快照与本地缓冲对齐。
 *
 * 快照是权威结构（工具卡片、消息顺序都来自它），缓冲只补正文。正文按
 * text part 对齐：ordinal 就是该消息第几个 text part，因此多段正文之间夹
 * 着工具调用时也不会把整段文本拼错。快照还没有 parts（例如用户消息）时
 * 退回整条消息的重叠合并。
 */
export function mergeSnapshot(
	snapshot: Message[],
	buffer: LiveBuffer,
): { messages: Message[]; buffer: LiveBuffer } {
	const remaining = new Map(buffer.parts);
	const matched = new Set<string>();

	const messages = snapshot.map((message) => {
		const liveParts = remaining.get(message.id);
		if (!liveParts || liveParts.size === 0) return message;
		matched.add(message.id);

		const base = messageText(message);
		const textParts = snapshotTextParts(message);
		// 只有 text 恰好是 text parts 拼接时，ordinal 才能安全映射到 parts。
		const aligned = textParts.length > 0 && textParts.join("") === base;

		if (!aligned) {
			const live = bufferText(liveParts);
			const authoritative = [...liveParts.values()].every((part) => part.ended);
			const merged = authoritative ? live : mergeLiveText(base, live);
			if (merged === base) {
				remaining.delete(message.id);
				return message;
			}
			return { ...message, text: merged };
		}

		const kept = new Map(liveParts);
		const texts = [...textParts];
		let changed = false;
		for (const [ordinal, live] of liveParts) {
			const current = texts[ordinal] ?? "";
			if (live.ended) {
				// ended 是该 part 的完整文本，可以直接替代快照里的旧前缀。
				if (live.text !== current) {
					texts[ordinal] = live.text;
					changed = true;
				}
				if (live.text === current) kept.delete(ordinal);
				continue;
			}
			const merged = mergeLiveText(current, live.text);
			if (merged !== current) {
				texts[ordinal] = merged;
				changed = true;
			}
			// 快照已经覆盖这段增量，缓冲可以丢掉；后续 delta 会重新累积。
			if (merged === current) kept.delete(ordinal);
		}

		if (kept.size === 0) remaining.delete(message.id);
		else if (kept.size !== liveParts.size) remaining.set(message.id, kept);

		return changed ? { ...message, text: texts.join("") } : message;
	});

	for (const [id, parts] of remaining) {
		if (matched.has(id)) continue;
		const text = bufferText(parts);
		if (text === "") continue;
		messages.push({ id, role: "assistant", text });
	}

	return { messages, buffer: { parts: remaining } };
}
