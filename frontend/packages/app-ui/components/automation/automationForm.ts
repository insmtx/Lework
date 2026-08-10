"use client";

import type {
	BackendAutomationCalendarConfig,
	BackendAutomationScheduleFormConfig,
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
	/** 锚点时刻（本地时区 HH:MM），固定间隔从该时刻起按周期推进，缺省 00:00 */
	anchorTime: string;
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
		anchorTime: "00:00",
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
				anchorTime: extractAnchorTime(interval?.anchor_at),
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
): BackendAutomationScheduleFormConfig {
	if (state.mode === "interval") {
		return {
			mode: "interval",
			timezone,
			interval: {
				interval_minutes: state.interval.intervalMinutes,
				interval_unit: "minute",
				interval_seconds: state.interval.intervalMinutes * 60,
				// 锚点以本地 HH:MM 提交，服务端按所选时区解释
				anchor_at: state.interval.anchorTime,
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

/** 从后端 anchor_at（HH:MM 或完整时间戳）中提取本地 HH:MM 用于表单回显 */
export function extractAnchorTime(anchorAt: string | undefined): string {
	if (!anchorAt) return "00:00";
	const m = /^(\d{1,2}):(\d{2})/.exec(anchorAt.trim());
	if (m && m[1] !== undefined && m[2] !== undefined) {
		return `${m[1].padStart(2, "0")}:${m[2].padStart(2, "0")}`;
	}
	const date = new Date(anchorAt);
	if (!Number.isNaN(date.getTime())) {
		return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
	}
	return "00:00";
}

/** 取得今天「时:分」对应的 Date（浏览器本地时区） */
function todayAt(hhmm: string): Date {
	const m = /^(\d{1,2}):(\d{2})/.exec(hhmm.trim());
	if (!m) return new Date(NaN);
	const d = new Date();
	d.setHours(Number(m[1]), Number(m[2]), 0, 0);
	return d;
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

const WEEKDAY_LABELS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

function weekDaysText(days: number[]): string {
	return days
		.map((d) => WEEKDAY_LABELS[d] ?? "")
		.filter(Boolean)
		.join("、");
}

function monthDaysText(days: number[]): string {
	return days.map((d) => `${d}日`).join("、");
}

/** 计算下一次执行时间的客户端预览（浏览器时区近似，最终以服务端为准） */
export function computeNextRunPreview(
	state: AutomationScheduleFormState,
	from = new Date(),
): Date | null {
	const now = from.getTime();
	if (state.mode === "interval") {
		const intervalMs = state.interval.intervalMinutes * 60_000;
		const anchor = todayAt(state.interval.anchorTime);
		// 从锚点时刻起按周期推进到严格晚于 now 的下一次 occurrence
		if (Number.isNaN(anchor.getTime())) {
			return new Date(now + intervalMs);
		}
		let next = anchor.getTime();
		while (next <= now) {
			next += intervalMs;
		}
		return new Date(next);
	}
	if (state.calendar.preset === "hourly") {
		const next = new Date(now);
		next.setSeconds(0, 0);
		next.setMinutes(state.calendar.minute);
		if (next.getTime() <= now) {
			next.setHours(next.getHours() + 1, state.calendar.minute, 0, 0);
		}
		return next;
	}
	const hour = state.calendar.hour;
	const minute = state.calendar.minute;
	// 逐日向后探测（上限 400 天）
	for (let i = 0; i < 400; i++) {
		const base = new Date(now);
		base.setDate(base.getDate() + i);
		if (!matchesCalendarDay(base, state.calendar)) continue;
		const cand = new Date(base);
		cand.setHours(hour, minute, 0, 0);
		if (cand.getTime() > now) return cand;
	}
	return null;
}

function matchesCalendarDay(date: Date, c: AutomationCalendarFormState): boolean {
	switch (c.preset) {
		case "daily":
			return true;
		case "weekly":
			return c.daysOfWeek.includes(date.getDay());
		case "monthly": {
			// 月末回退：目标月份没有该日期时命中最后一天
			const lastDay = new Date(date.getFullYear(), date.getMonth() + 1, 0).getDate();
			return c.daysOfMonth.some((d) => {
				if (d <= lastDay) return date.getDate() === d;
				return date.getDate() === lastDay && lastDay < d;
			});
		}
		default:
			return false;
	}
}
