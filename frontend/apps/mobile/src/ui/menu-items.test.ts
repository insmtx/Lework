import { describe, expect, it } from "vitest";
import { MENU_ITEMS } from "./menu-items";

describe("MENU_ITEMS", () => {
	it("顺序固定为项目、待审批、节点、设置", () => {
		expect(MENU_ITEMS.map((item) => item.key)).toEqual([
			"projects",
			"pending",
			"nodes",
			"settings",
		]);
	});

	it("href 唯一，避免两项跳到同一页", () => {
		const hrefs = MENU_ITEMS.map((item) => item.href);
		expect(new Set(hrefs).size).toBe(hrefs.length);
	});

	it("只有待审批带计数角标", () => {
		expect(MENU_ITEMS.filter((item) => item.badge === "pending")).toHaveLength(1);
		expect(MENU_ITEMS.find((item) => item.badge === "pending")?.key).toBe("pending");
	});
});
