export function formatTime(timestamp: number): string {
	const date = new Date(timestamp);
	return date.toLocaleTimeString("zh-CN", {
		hour: "2-digit",
		minute: "2-digit",
	});
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
