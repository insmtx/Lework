import { describe, expect, it } from "vitest";

import {
	getRecentProjectsForLeftRail,
	getVisibleLeftRailItems,
	LEFT_RAIL_LIST_PREVIEW_LIMIT,
} from "./left-rail-list-utils";

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

describe("getRecentProjectsForLeftRail", () => {
	it("会保留已展开项目，避免被最近项目数量限制挤出列表", () => {
		const projects = [
			{ id: "project-1", updatedAt: 100 },
			{ id: "project-2", updatedAt: 90 },
			{ id: "project-3", updatedAt: 80 },
			{ id: "project-4", updatedAt: 70 },
			{ id: "project-5", updatedAt: 60 },
			{ id: "project-6", updatedAt: 50 },
		];

		const visibleProjects = getRecentProjectsForLeftRail(projects, new Set(["project-6"]), 5);

		expect(visibleProjects.map((project) => project.id)).toEqual([
			"project-1",
			"project-2",
			"project-3",
			"project-4",
			"project-6",
		]);
	});

	it("会按更新时间优先补足非展开项目", () => {
		const projects = [
			{ id: "project-1", updatedAt: 100 },
			{ id: "project-2", updatedAt: 90 },
			{ id: "project-3", updatedAt: 80 },
			{ id: "project-4", updatedAt: 70 },
			{ id: "project-5", updatedAt: 60 },
			{ id: "project-6", updatedAt: 50 },
			{ id: "project-7", updatedAt: 40 },
		];

		const visibleProjects = getRecentProjectsForLeftRail(
			projects,
			new Set(["project-6", "project-7"]),
			5,
		);

		expect(visibleProjects.map((project) => project.id)).toEqual([
			"project-1",
			"project-2",
			"project-3",
			"project-6",
			"project-7",
		]);
	});
});
