// nextRetryDelay 返回第 attempt 次重连前的等待时间（毫秒）。
// 1s 起指数退避、封顶 30s：移动网络切换时不要疯狂重试，也不要等太久。
export function nextRetryDelay(attempt: number): number {
	const safe = Math.max(0, Math.floor(attempt));
	return Math.min(1000 * 2 ** safe, 30_000);
}
