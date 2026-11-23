/**
 * LatestRequestMap 给「同一资源的连续乐观写入」编号，只让最后一次请求的
 * 结果落到界面上。
 *
 * 用户可能在请求返回前连续改两次颜色：慢请求 A 失败、后发的 B 成功时，
 * 若 A 的回滚仍然生效，界面会回到更早的颜色并与服务端分叉。按 key 记录
 * 最新序号即可丢弃所有过期结果；不同节点互不影响。
 */
export class LatestRequestMap {
	private sequence = new Map<string, number>();

	begin(key: string): number {
		const next = (this.sequence.get(key) ?? 0) + 1;
		this.sequence.set(key, next);
		return next;
	}

	isCurrent(key: string, token: number): boolean {
		return this.sequence.get(key) === token;
	}
}
