const WEEKDAY_NAMES = [
	"\u661f\u671f\u65e5",
	"\u661f\u671f\u4e00",
	"\u661f\u671f\u4e8c",
	"\u661f\u671f\u4e09",
	"\u661f\u671f\u56db",
	"\u661f\u671f\u4e94",
	"\u661f\u671f\u516d",
] as const;

function pad2(value: number): string {
	return String(value).padStart(2, "0");
}

function startOfLocalDay(date: Date): number {
	return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

function formatClockTime(date: Date): string {
	return `${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}

/** 按微信会话消息时间规则展示：今天 / 昨天 / 近7天星期 / 今年月日 / 跨年年月日。 */
export function formatTime(timestamp: number): string {
	const date = new Date(timestamp);
	const now = new Date();
	const timePart = formatClockTime(date);
	const dayDiff = Math.floor((startOfLocalDay(now) - startOfLocalDay(date)) / 86_400_000);

	if (dayDiff <= 0) {
		return timePart;
	}
	if (dayDiff === 1) {
		return `\u6628\u5929 ${timePart}`;
	}
	if (dayDiff < 7) {
		return `${WEEKDAY_NAMES[date.getDay()]} ${timePart}`;
	}
	if (date.getFullYear() === now.getFullYear()) {
		return `${date.getMonth() + 1}\u6708${date.getDate()}\u65e5 ${timePart}`;
	}
	return `${date.getFullYear()}\u5e74${date.getMonth() + 1}\u6708${date.getDate()}\u65e5 ${timePart}`;
}

export function formatDate(timestamp: number): string {
	const date = new Date(timestamp);
	const isToday = date.toDateString() === new Date().toDateString();
	if (isToday) {
		return `\u4eca\u5929 ${formatTime(timestamp)}`;
	}
	return date.toLocaleDateString("zh-CN", {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	});
}

export function formatArtifactTime(timestamp?: number): string {
	if (!timestamp || !Number.isFinite(timestamp)) return "";
	return formatDate(timestamp);
}

export function parseOptionalTimestamp(value?: string): number | undefined {
	if (!value) return undefined;
	const normalized = value.trim();
	if (!normalized || normalized.startsWith("0001-01-01")) return undefined;

	const timestamp = new Date(normalized).getTime();
	return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : undefined;
}

export function formatFileSize(bytes: number): string {
	if (bytes < 1024) return `${bytes}B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
	return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

export function formatTokenCount(count: number): string {
	if (!count) return "0";
	if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`;
	if (count >= 1000) return `${(count / 1000).toFixed(1)}K`;
	return String(count);
}

export function formatLatency(ms: number): string {
	if (!Number.isFinite(ms) || ms <= 0) return "0ms";
	if (ms >= 1000) {
		const seconds = ms / 1000;
		return seconds >= 10 ? `${Math.round(seconds)}s` : `${seconds.toFixed(1)}s`;
	}
	return `${Math.round(ms)}ms`;
}
