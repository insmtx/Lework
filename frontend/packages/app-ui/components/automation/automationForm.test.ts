import type { BackendAutomationScheduleFormConfig } from "@leros/store";
import { describe, expect, it } from "vitest";
import {
	buildFormSummary,
	buildScheduleFormState,
	buildScheduleRequest,
	computeNextRunPreview,
	formatSelectionPreview,
	toggleCalendarArray,
	weekDaysText,
} from "./automationForm";

/** 构造后端 form_config：mode + calendar 包装 */
function calendarConfig(calendar: {
	preset: string;
	hour: number;
	minute: number;
	days_of_week?: number[];
	days_of_month?: number[];
}): BackendAutomationScheduleFormConfig {
	return { mode: "calendar", calendar };
}

describe("toggleCalendarArray", () => {
	it("勾选新增、取消移除", () => {
		expect(toggleCalendarArray([1], 2, { order: "numeric" })).toEqual([1, 2]);
		expect(toggleCalendarArray([1, 2], 1, { order: "numeric" })).toEqual([2]);
	});

	it("始终去重", () => {
		expect(toggleCalendarArray([1, 2], 2, { order: "numeric" })).toEqual([1]);
		expect(toggleCalendarArray([1, 2], 1, { order: "numeric" })).toEqual([2]);
	});

	it("不允许取消唯一的选中项，返回原数组", () => {
		const result = toggleCalendarArray([3], 3, { order: "numeric" });
		expect(result).toEqual([3]);
	});

	it("勾选后至少保留一项", () => {
		// 先清空仅剩的一项失败，再新增仍有效
		expect(toggleCalendarArray([5], 5, { order: "numeric" })).toEqual([5]);
		expect(toggleCalendarArray([5], 7, { order: "numeric" })).toEqual([5, 7]);
	});

	it("weekday 顺序：周日(0)始终排在末尾，其余按 1-6 升序", () => {
		expect(toggleCalendarArray([3, 1], 0, { order: "weekday" })).toEqual([1, 3, 0]);
		expect(toggleCalendarArray([1, 0, 3, 2], 5, { order: "weekday" })).toEqual([1, 2, 3, 5, 0]);
	});

	it("numeric 顺序按数值升序归一化", () => {
		// 依次选择 31、15，应归一化为升序 15、31
		expect(toggleCalendarArray([], 31, { order: "numeric" })).toEqual([31]);
		expect(toggleCalendarArray([31], 15, { order: "numeric" })).toEqual([15, 31]);
		expect(toggleCalendarArray([2, 31], 15, { order: "numeric" })).toEqual([2, 15, 31]);
	});
});

describe("formatSelectionPreview", () => {
	const mondayLabel = (d: number) => weekDaysText([d]);

	it("1 项显示名称", () => {
		expect(formatSelectionPreview([1], mondayLabel)).toBe("周一");
	});

	it("2 项显示名称并以顿号连接", () => {
		expect(formatSelectionPreview([1, 3], mondayLabel)).toBe("周一、周三");
	});

	it("3 项及以上显示数量", () => {
		expect(formatSelectionPreview([1, 2, 3], mondayLabel)).toBe("已选 3 项");
		expect(formatSelectionPreview([1, 2, 3, 4, 5], mondayLabel)).toBe("已选 5 项");
	});

	it("空数组显示未选择", () => {
		expect(formatSelectionPreview([], mondayLabel)).toBe("未选择");
	});
});

describe("buildFormSummary", () => {
	it("weekly 完整列出多个星期", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "weekly", hour: 8, minute: 30, days_of_week: [1, 3, 0] }),
		);
		expect(buildFormSummary(state)).toBe("每周周一、周三、周日 08:30");
	});

	it("monthly 完整列出多个日期", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "monthly", hour: 9, minute: 0, days_of_month: [1, 15] }),
		);
		expect(buildFormSummary(state)).toBe("每月1日、15日 09:00");
	});
});

describe("buildScheduleRequest", () => {
	it("weekly 提交完整 days_of_week 数组", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "weekly", hour: 8, minute: 30, days_of_week: [1, 3, 5] }),
		);
		const req = buildScheduleRequest(state, "Asia/Shanghai");
		if (req.mode === "interval") throw new Error("unexpected");
		expect(req.calendar?.preset).toBe("weekly");
		expect(req.calendar?.days_of_week).toEqual([1, 3, 5]);
	});

	it("monthly 提交完整 days_of_month 数组", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "monthly", hour: 9, minute: 0, days_of_month: [2, 14, 31] }),
		);
		const req = buildScheduleRequest(state, "UTC");
		if (req.mode === "interval") throw new Error("unexpected");
		expect(req.calendar?.preset).toBe("monthly");
		expect(req.calendar?.days_of_month).toEqual([2, 14, 31]);
	});

	it("interval 请求不再提交 anchor_at", () => {
		const state = buildScheduleFormState({
			mode: "interval",
			interval: { interval_minutes: 30 },
		});
		const req = buildScheduleRequest(state, "Asia/Shanghai");
		if (req.mode !== "interval") throw new Error("unexpected");
		expect(req.interval).toEqual({
			interval_minutes: 30,
			interval_unit: "minute",
			interval_seconds: 1800,
		});
		expect("anchor_at" in (req.interval ?? {})).toBe(false);
	});
});

describe("computeNextRunPreview", () => {
	// 2026-08-11 是周二。用固定基准时间便于断言。
	const base = new Date(2026, 7, 11, 0, 0, 0); // 2026-08-11 00:00 本地

	it("interval 从当前时刻开始计算，不依赖锚点", () => {
		const state = buildScheduleFormState({
			mode: "interval",
			interval: { interval_minutes: 30 },
		});
		const next = computeNextRunPreview(state, base);
		expect(next?.getTime()).toBe(base.getTime() + 30 * 60_000);
	});

	it("多星期命中最近的选中星期", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "weekly", hour: 9, minute: 0, days_of_week: [3, 5] }), // 周三、周五
		);
		const next = computeNextRunPreview(state, base);
		// 2026-08-11 周二 00:00 之后的下一个是周三 2026-08-12 09:00
		expect(next?.getDay()).toBe(3);
		expect(next?.getFullYear()).toBe(2026);
		expect(next?.getMonth()).toBe(7);
		expect(next?.getDate()).toBe(12);
		expect(next?.getHours()).toBe(9);
	});

	it("选中了今天则命中的是同样的星期（当天稍后时刻）", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "weekly", hour: 8, minute: 0, days_of_week: [2] }), // 周二，base 也是周二
		);
		const next = computeNextRunPreview(state, base);
		expect(next?.getDay()).toBe(2);
		expect(next?.getDate()).toBe(11);
		expect(next?.getHours()).toBe(8);
	});

	it("月末回退：目标月份没有该日期时命中最后一天", () => {
		// 2026-02 没有 30 日，应命中 2026-02-28 09:00
		const state = buildScheduleFormState(
			calendarConfig({ preset: "monthly", hour: 9, minute: 0, days_of_month: [30] }),
		);
		const feb = new Date(2026, 1, 1, 0, 0, 0); // 2026-02-01
		const next = computeNextRunPreview(state, feb);
		expect(next?.getMonth()).toBe(1); // 2 月
		expect(next?.getDate()).toBe(28);
		expect(next?.getHours()).toBe(9);
	});

	it("按任务时区计算日历预览，再返回绝对时间", () => {
		const state = buildScheduleFormState(calendarConfig({ preset: "daily", hour: 9, minute: 0 }));
		const from = new Date("2026-08-10T23:00:00Z"); // 上海 8 月 11 日 07:00
		const next = computeNextRunPreview(state, "Asia/Shanghai", from);
		expect(next?.toISOString()).toBe("2026-08-11T01:00:00.000Z");
	});
});

describe("buildScheduleFormState", () => {
	it("缺失 days_of_week / days_of_month 时回退默认单值", () => {
		const state = buildScheduleFormState(calendarConfig({ preset: "daily", hour: 9, minute: 0 }));
		expect(state.calendar.daysOfWeek).toEqual([1]);
		expect(state.calendar.daysOfMonth).toEqual([1]);
	});

	it("保留所选数组不回退", () => {
		const state = buildScheduleFormState(
			calendarConfig({ preset: "weekly", hour: 9, minute: 0, days_of_week: [1, 4, 6] }),
		);
		expect(state.calendar.daysOfWeek).toEqual([1, 4, 6]);
	});
});
