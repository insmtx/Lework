"use client";

import type {
	BackendAutomationCalendarConfig,
	BackendAutomationScheduleFormConfig,
	BackendAutomationScheduleInput,
} from "@leros/store";

export type AutomationCalendarFormState = {
	preset: "daily" | "weekly" | "monthly" | "hourly";
	hour: number;
	minute: number;
	daysOfWeek: number[];
	daysOfMonth: number[];
};

export type AutomationIntervalFormState = {
	intervalMinutes: number;
};

export type AutomationScheduleFormState = {
	mode: "calendar" | "interval";
	calendar: AutomationCalendarFormState;
	interval: AutomationIntervalFormState;
};

export const DEFAULT_SCHEDULE_FORM: AutomationScheduleFormState = {
	mode: "calendar",
	calendar: {
		preset: "daily",
		hour: 9,
		minute: 0,
		daysOfWeek: [1],
		daysOfMonth: [1],
	},
	interval: {
		intervalMinutes: 30,
	},
};

/** 从后端 schedule_spec.form_config 回填表单状态 */
export function buildScheduleFormState(
	formConfig?: BackendAutomationScheduleFormConfig,
): AutomationScheduleFormState {
	if (formConfig?.mode === "interval") {
		const interval = formConfig.interval;
		const intervalSeconds = interval?.interval_seconds ?? (interval?.interval_minutes ?? 30) * 60;
		return {
			mode: "interval",
			calendar: DEFAULT_SCHEDULE_FORM.calendar,
			interval: {
				intervalMinutes: Math.max(1, Math.round(intervalSeconds / 60)),
			},
		};
	}
	const calendar = formConfig?.calendar;
	return {
		mode: "calendar",
		calendar: {
			preset: (calendar?.preset as "daily" | "weekly" | "monthly" | "hourly") ?? "daily",
			hour: calendar?.hour ?? 9,
			minute: calendar?.minute ?? 0,
			daysOfWeek: calendar?.days_of_week?.length ? calendar.days_of_week : [1],
			daysOfMonth: calendar?.days_of_month?.length ? calendar.days_of_month : [1],
		},
		interval: DEFAULT_SCHEDULE_FORM.interval,
	};
}

/** 将表单状态编译回后端要求的 schedule 提交结构 */
export function buildScheduleRequest(
	state: AutomationScheduleFormState,
	timezone: string,
): BackendAutomationScheduleInput {
	if (state.mode === "interval") {
		return {
			mode: "interval",
			timezone,
			interval: {
				interval_minutes: state.interval.intervalMinutes,
				interval_unit: "minute",
				interval_seconds: state.interval.intervalMinutes * 60,
			},
		};
	}
	const c = state.calendar;
	const calendarConfig: BackendAutomationCalendarConfig = {
		preset: c.preset,
		hour: c.hour,
		minute: c.minute,
	};
	return {
		mode: "calendar",
		timezone,
		calendar:
			c.preset === "weekly"
				? { ...calendarConfig, days_of_week: c.daysOfWeek }
				: c.preset === "monthly"
					? { ...calendarConfig, days_of_month: c.daysOfMonth }
					: calendarConfig,
	};
}

export function getBrowserTimezone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Shanghai";
	} catch {
		return "Asia/Shanghai";
	}
}

/** 生成周期摘要（用于表单预览） */
export function buildFormSummary(state: AutomationScheduleFormState): string {
	const timeDesc = formatTime(state.calendar.hour, state.calendar.minute);
	if (state.mode === "interval") {
		return `每 ${state.interval.intervalMinutes} 分钟执行一次`;
	}
	switch (state.calendar.preset) {
		case "daily":
			return `每天 ${timeDesc}`;
		case "weekly":
			return `每周${weekDaysText(state.calendar.daysOfWeek)} ${timeDesc}`;
		case "monthly":
			return `每月${monthDaysText(state.calendar.daysOfMonth)} ${timeDesc}`;
		case "hourly":
			return `每小时 ${String(state.calendar.minute).padStart(2, "0")} 分执行`;
		default:
			return "";
	}
}

export function formatTime(hour: number, minute: number): string {
	return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

export const WEEKDAY_LABELS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

export function weekDaysText(days: number[]): string {
	return days
		.map((d) => WEEKDAY_LABELS[d] ?? "")
		.filter(Boolean)
		.join("、");
}

export function monthDaysText(days: number[]): string {
	return days.map((d) => `${d}日`).join("、");
}

/** 将界面固定顺序要求映射到数组：周日(0)排在数组末尾，其余按 1-6 升序 */
function sortWithSundayLast(days: number[]): number[] {
	return [...days].sort((a, b) => {
		if (a === 0) return 1;
		if (b === 0) return -1;
		return a - b;
	});
}

/**
 * 在日历数组中切换某个值：勾选则追加、取消则移除。
 * 始终去重并按界面固定顺序返回；至少保留一项，不允许取消唯一的选中项（返回原数组）。
 * - weekday：周一到周六按 1-6 升序，周日(0)排在末尾
 * - numeric：按数值升序（每月日期 1-31）
 */
export function toggleCalendarArray(
	arr: number[],
	value: number,
	{ order }: { order: "weekday" | "numeric" },
): number[] {
	const next = arr.includes(value) ? arr.filter((d) => d !== value) : [...arr, value];
	if (next.length === 0) return arr;
	const deduped = Array.from(new Set(next));
	return order === "weekday" ? sortWithSundayLast(deduped) : [...deduped].sort((a, b) => a - b);
}

/**
 * 生成下拉触发区域的选中摘要（完整选项由 `weekDaysText`/`monthDaysText` 在周期摘要中展示）：
 * 选中 1-2 项显示名称（“周一、周三”），3 项及以上显示“已选 N 项”。
 */
export function formatSelectionPreview(days: number[], formatter: (d: number) => string): string {
	if (days.length === 0) return "未选择";
	if (days.length >= 3) return `已选 ${days.length} 项`;
	return days.map(formatter).join("、");
}

/** 计算下一次执行时间；日历规则先按任务时区计算，再返回绝对时间。 */
export function computeNextRunPreview(
	state: AutomationScheduleFormState,
	timezoneOrFrom: string | Date = getBrowserTimezone(),
	maybeFrom = new Date(),
): Date | null {
	const timezone = timezoneOrFrom instanceof Date ? getBrowserTimezone() : timezoneOrFrom;
	const from = timezoneOrFrom instanceof Date ? timezoneOrFrom : maybeFrom;
	const now = from.getTime();
	if (state.mode === "interval") {
		const intervalMs = state.interval.intervalMinutes * 60_000;
		return new Date(now + intervalMs);
	}
	const formatter = createZonedFormatter(timezone);
	if (!formatter) return new Date(now);
	const current = zonedParts(from, timezone, formatter);
	if (!current) return new Date(now);
	let hourStart = state.calendar.preset === "hourly" ? current.hour : state.calendar.hour;
	const hourEnd = state.calendar.preset === "hourly" ? 23 : state.calendar.hour;
	// 逐日向后探测（上限 400 天）
	for (let i = 0; i < 400; i++) {
		const base = calendarDate(current.year, current.month, current.day, i);
		if (
			!base ||
			(state.calendar.preset !== "hourly" && !matchesCalendarDay(base, state.calendar))
		) {
			continue;
		}
		for (let hour = hourStart; hour <= hourEnd; hour++) {
			const candidate = zonedDateTimeToUtc(base, hour, state.calendar.minute, timezone, formatter);
			if (candidate && candidate.getTime() > now) return candidate;
		}
		if (state.calendar.preset === "hourly") hourStart = 0;
	}
	return null;
}

function matchesCalendarDay(date: Date, c: AutomationCalendarFormState): boolean {
	switch (c.preset) {
		case "daily":
			return true;
		case "weekly":
			return c.daysOfWeek.includes(date.getUTCDay());
		case "monthly": {
			// 月末回退：目标月份没有该日期时命中最后一天
			const lastDay = new Date(
				Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 0),
			).getUTCDate();
			return c.daysOfMonth.some((d) => {
				if (d <= lastDay) return date.getUTCDate() === d;
				return date.getUTCDate() === lastDay && lastDay < d;
			});
		}
		default:
			return false;
	}
}

type ZonedParts = { year: number; month: number; day: number; hour: number; minute: number };

type ZonedFormatter = Intl.DateTimeFormat;

function createZonedFormatter(timezone: string): ZonedFormatter | null {
	try {
		return new Intl.DateTimeFormat("en-US", {
			timeZone: timezone,
			year: "numeric",
			month: "2-digit",
			day: "2-digit",
			hour: "2-digit",
			minute: "2-digit",
			hourCycle: "h23",
		});
	} catch {
		return null;
	}
}

function zonedParts(
	date: Date,
	timezone: string,
	formatter = createZonedFormatter(timezone),
): ZonedParts | null {
	try {
		if (!formatter) return null;
		const parts = formatter.formatToParts(date);
		const values = Object.fromEntries(
			parts.map((part) => [part.type, Number(part.value)]),
		) as Record<string, number>;
		const year = values.year ?? NaN;
		const month = values.month ?? NaN;
		const day = values.day ?? NaN;
		const hour = values.hour ?? NaN;
		const minute = values.minute ?? NaN;
		if (![year, month, day, hour, minute].every(Number.isFinite)) return null;
		return { year, month, day, hour, minute };
	} catch {
		return null;
	}
}

function calendarDate(year: number, month: number, day: number, offsetDays: number): Date | null {
	const value = new Date(Date.UTC(year, month - 1, day + offsetDays));
	return Number.isNaN(value.getTime()) ? null : value;
}

function zonedDateTimeToUtc(
	date: Date,
	hour: number,
	minute: number,
	timezone: string,
	formatter?: ZonedFormatter,
): Date | null {
	const target = Date.UTC(
		date.getUTCFullYear(),
		date.getUTCMonth(),
		date.getUTCDate(),
		hour,
		minute,
		0,
		0,
	);
	let guess = target;
	for (let i = 0; i < 4; i++) {
		const actual = zonedParts(new Date(guess), timezone, formatter);
		if (!actual) return null;
		const actualAsUtc = Date.UTC(
			actual.year,
			actual.month - 1,
			actual.day,
			actual.hour,
			actual.minute,
			0,
			0,
		);
		guess += target - actualAsUtc;
	}
	const result = new Date(guess);
	const actual = zonedParts(result, timezone, formatter);
	if (
		!actual ||
		actual.year !== date.getUTCFullYear() ||
		actual.month !== date.getUTCMonth() + 1 ||
		actual.day !== date.getUTCDate() ||
		actual.hour !== hour ||
		actual.minute !== minute
	) {
		return null;
	}
	return result;
}
