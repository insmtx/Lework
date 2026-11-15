import { describe, expect, it } from "vitest";

import {
	advance,
	isUnread,
	parseWatermarks,
	pruneWatermarks,
	READ_KEY,
	serializeWatermarks,
	UNREAD_GRACE_MS,
	watermarkKey,
} from "./read-state";

describe("watermarkKey", () => {
	it("按节点与会话拼接", () => {
		expect(watermarkKey("node_1", "ses_1")).toBe("node_1:ses_1");
	});
});

describe("advance", () => {
	it("advance 只前进", () => {
		const watermarks = { "node_1:ses_1": 100 };
		expect(advance(watermarks, "node_1:ses_1", 50)).toEqual({ "node_1:ses_1": 100 });

		const next = advance(watermarks, "node_1:ses_1", 200);
		expect(next["node_1:ses_1"]).toBe(200);
		// 不修改入参：React 靠引用变化判断是否需要写入。
		expect(watermarks["node_1:ses_1"]).toBe(100);
	});

	it("新键直接写入", () => {
		expect(advance({}, "node_1:ses_2", 42)).toEqual({ "node_1:ses_2": 42 });
	});
});

describe("pruneWatermarks", () => {
	const many = (count: number): Record<string, number> => {
		const result: Record<string, number> = {};
		for (let index = 0; index < count; index += 1) {
			result[`node_1:ses_${index}`] = index + 1;
		}
		return result;
	};

	it("prune 保留最近 40 条", () => {
		const pruned = pruneWatermarks(many(45), []);

		expect(Object.keys(pruned)).toHaveLength(40);
		expect(pruned["node_1:ses_0"]).toBeUndefined();
		expect(pruned["node_1:ses_44"]).toBe(45);
	});

	it("显式可见的键优先保留，再按最近使用补齐", () => {
		const pruned = pruneWatermarks(many(45), ["node_1:ses_0"]);

		expect(Object.keys(pruned)).toHaveLength(40);
		expect(pruned["node_1:ses_0"]).toBe(1);
		expect(pruned["node_1:ses_5"]).toBeUndefined();
	});

	it("上限以内原样返回", () => {
		expect(pruneWatermarks({ "node_1:ses_1": 1 }, [])).toEqual({ "node_1:ses_1": 1 });
	});
});

describe("isUnread", () => {
	const watermark = Date.parse("2026-11-15T09:59:30Z");

	it("isUnread 需要超过 60s 宽限", () => {
		expect(isUnread("2026-11-15T10:00:00Z", watermark, 0)).toBe(false);
		expect(isUnread("2026-11-15T10:02:00Z", watermark, 0)).toBe(true);
	});

	it("恰好等于宽限边界时不算未读", () => {
		const at = watermark + UNREAD_GRACE_MS;
		expect(isUnread(new Date(at).toISOString(), watermark, 0)).toBe(false);
	});

	it("isUnread 缺少 updatedAt 时为 false", () => {
		expect(isUnread(undefined, watermark, 0)).toBe(false);
		expect(isUnread("", watermark, 0)).toBe(false);
		expect(isUnread("not-a-date", watermark, 0)).toBe(false);
	});

	it("没有水位时不做未读标记", () => {
		// 从未打开过的会话不该一进列表就挂未读点：水位缺失时以当前时间兜底。
		const now = Date.parse("2026-11-15T10:10:00Z");
		expect(isUnread("2026-11-15T10:02:00Z", undefined, now)).toBe(false);
	});
});

describe("parseWatermarks / serializeWatermarks", () => {
	it("紧凑编码可回读", () => {
		const watermarks = { "node_1:ses_1": 1_700_000_000_000 };
		expect(serializeWatermarks(watermarks)).toBe('{"node_1:ses_1":1700000000000}');
		expect(parseWatermarks(serializeWatermarks(watermarks))).toEqual(watermarks);
	});

	it("parseWatermarks 容忍坏 JSON", () => {
		expect(parseWatermarks(null)).toEqual({});
		expect(parseWatermarks("")).toEqual({});
		expect(parseWatermarks("not json")).toEqual({});
		expect(parseWatermarks("[1,2]")).toEqual({});
	});

	it("丢弃非法条目", () => {
		expect(parseWatermarks('{"ok":1,"str":"x","nan":null,"obj":{}}')).toEqual({ ok: 1 });
	});

	it("READ_KEY 是可写进 SecureStore 的键", () => {
		expect(READ_KEY).toBe("lework.read.v1");
		expect(READ_KEY).toMatch(/^[A-Za-z0-9._-]+$/);
	});
});
