// 已读水位是本机偏好，不进控制面：单设备不同步，也不需要第二个服务端字段。
//
// 存 SecureStore（App 里没有 AsyncStorage/MMKV），所以必须紧凑：只保留最近
// 使用的 40 条，值控制在 ~1.5 KB 以内。丢掉的旧条目最坏结果是一次误报未读，
// 远好过因为超长而静默写失败。
export type Watermarks = Record<string, number>;

/** READ_KEY 是 SecureStore 的键，只允许字母数字与 ._-。 */
export const READ_KEY = "lework.read.v1";

/**
 * UNREAD_GRACE_MS 吸收手机与服务端之间的时钟偏差。
 *
 * updatedAt 来自 OpenCode 所在机器的时钟，水位来自手机时钟；没有宽限会出现
 * 「刚看完又变未读」。代价是看完后 1 分钟内到达的新内容不报未读，而那时
 * 用户通常仍在页面上。
 */
export const UNREAD_GRACE_MS = 60_000;

/** MAX_WATERMARKS 与 SecureStore 单值容量之间的折中。 */
const MAX_WATERMARKS = 40;

/** watermarkKey 让水位按「节点 + 会话」定位，避免跨节点 id 冲突。 */
export function watermarkKey(nodeID: string, sessionID: string): string {
	return `${nodeID}:${sessionID}`;
}

/** parseWatermarks 宽松解析：坏 JSON 或非法条目一律忽略，不让存储问题挡住列表。 */
export function parseWatermarks(raw: string | null): Watermarks {
	if (!raw) return {};
	let parsed: unknown;
	try {
		parsed = JSON.parse(raw);
	} catch {
		return {};
	}
	if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};

	const watermarks: Watermarks = {};
	for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
		if (typeof value === "number" && Number.isFinite(value)) watermarks[key] = value;
	}
	return watermarks;
}

/** serializeWatermarks 输出无空格的 JSON，减少 SecureStore 单值长度。 */
export function serializeWatermarks(watermarks: Watermarks): string {
	return JSON.stringify(watermarks);
}

/**
 * advance 只把水位往前推。
 *
 * 两个会话页可能同时在返回栈里，较旧的页面完成加载时不能把水位往回拨，
 * 否则已经看过的内容会重新变成未读。
 */
export function advance(watermarks: Watermarks, key: string, at: number): Watermarks {
	const previous = watermarks[key];
	if (previous !== undefined && previous >= at) return watermarks;
	return { ...watermarks, [key]: at };
}

/**
 * pruneWatermarks 按最近使用保留最多 limit 条。
 *
 * keys 是当前列表里可见的会话（顺序即优先级）：它们是用户最可能继续看的，
 * 必须优先保留；剩余名额按水位时间倒序补齐。
 */
export function pruneWatermarks(
	watermarks: Watermarks,
	keys: readonly string[],
	limit: number = MAX_WATERMARKS,
): Watermarks {
	const kept: Watermarks = {};
	const keep = (key: string) => {
		if (Object.keys(kept).length >= limit) return;
		const at = watermarks[key];
		if (at === undefined || kept[key] !== undefined) return;
		kept[key] = at;
	};

	for (const key of keys) keep(key);

	if (Object.keys(kept).length < limit) {
		const rest = Object.keys(watermarks)
			.filter((key) => kept[key] === undefined)
			.sort((left, right) => watermarks[right] - watermarks[left]);
		for (const key of rest) keep(key);
	}

	return kept;
}

/**
 * isUnread 用更新时间与水位比较，决定是否显示未读点。
 *
 * 水位缺失时以 now 兜底：从未打开过的会话刚出现在列表里不该挂着未读点，
 * 否则首次安装后整个抽屉都是圆点。缺少/非法 updatedAt 同样不算未读。
 */
export function isUnread(
	updatedAt: string | undefined,
	watermark: number | undefined,
	now: number,
): boolean {
	if (!updatedAt) return false;
	const at = Date.parse(updatedAt);
	if (Number.isNaN(at)) return false;
	const since = watermark ?? now;
	return at > since + UNREAD_GRACE_MS;
}
