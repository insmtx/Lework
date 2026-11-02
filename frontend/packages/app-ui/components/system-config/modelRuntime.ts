import type { ModelItem } from "@leros/store";

const labels: Record<string, string> = {
	ready: "已就绪",
	starting: "启动中",
	draining: "停止中",
	stopping: "停止中",
	stopped: "已停止",
	error: "异常",
	unknown: "未知",
	unobserved: "未检测",
	available: "可用",
	unavailable: "不可用",
};

export function modelRuntimePresentation(item: ModelItem, now: number, refreshFailed = false) {
	const runtime = item.runtime;
	const source =
		runtime?.source === "spark" ? "Spark" : runtime?.source === "configuration" ? "配置" : "未知";
	let state = "unknown";
	let checkedAt = 0;
	let detail = runtime
		? "模型来源绑定不匹配，请联系管理员核对配置。"
		: "当前后端未提供来源和实际状态，请升级后端。";
	if (source === "Spark" && runtime) {
		checkedAt = runtime.observed_at;
		if (
			!refreshFailed &&
			checkedAt <= now &&
			runtime.valid_until > now &&
			runtime.valid_until - checkedAt <= 30
		)
			state = runtime.state;
		detail = "来自 Spark 管理服务的运行状态；已就绪不代表已允许提交新任务。";
	} else if (source === "配置") {
		const test = item.connectionTest;
		checkedAt = test?.observedAt ?? 0;
		state = !test
			? "unobserved"
			: test.observedAt <= now && test.validUntil > now
				? test.state
				: "unknown";
		detail = "点击操作菜单中的“测试连接”检测；结果有效期为 60 秒。";
	}
	if (refreshFailed) {
		state = "unknown";
		detail = "状态刷新失败，请检查连接后重试。";
	}
	const color =
		state === "ready" || state === "available"
			? "bg-emerald-500"
			: ["starting", "draining", "stopping"].includes(state)
				? "bg-amber-500"
				: ["error", "unavailable"].includes(state)
					? "bg-red-500"
					: "bg-slate-400";
	return {
		source,
		label: labels[state] ?? "未知",
		color,
		detail: checkedAt
			? `${detail} 最近检测：${new Date(checkedAt * 1000).toLocaleTimeString()}`
			: detail,
	};
}
