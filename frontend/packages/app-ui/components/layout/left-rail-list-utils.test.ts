import { describe, expect, it } from "vitest";

import { getVisibleLeftRailItems, LEFT_RAIL_LIST_PREVIEW_LIMIT } from "./left-rail-list-utils";

describe("getVisibleLeftRailItems", () => {
	it("未展开时最多只返回前六项，并显示展开入口", () => {
		const items = Array.from({ length: 8 }, (_, index) => `item-${index + 1}`);

		const result = getVisibleLeftRailItems(items, false);

		expect(result.visibleItems).toEqual(items.slice(0, LEFT_RAIL_LIST_PREVIEW_LIMIT));
		expect(result.showExpandTrigger).toBe(true);
	});

	it("数量不超过六项时保持原样且不显示展开入口", () => {
		const items = Array.from(
			{ length: LEFT_RAIL_LIST_PREVIEW_LIMIT },
			(_, index) => `item-${index + 1}`,
		);

		const result = getVisibleLeftRailItems(items, false);

		expect(result.visibleItems).toEqual(items);
		expect(result.showExpandTrigger).toBe(false);
	});

	it("展开后返回完整列表且隐藏展开入口", () => {
		const items = Array.from({ length: 9 }, (_, index) => `item-${index + 1}`);

		const result = getVisibleLeftRailItems(items, true);

		expect(result.visibleItems).toEqual(items);
		expect(result.showExpandTrigger).toBe(false);
	});
});
