// 更新检查的调度常量与纯判定：时间间隔放在这里统一维护，
// update-context 只消费，不各自写死毫秒数。

/** 前台定时检查间隔：30 分钟。 */
export const UPDATE_CHECK_INTERVAL_MS = 30 * 60 * 1000;

/** 从后台回到前台时的补查间隔：5 分钟内查过就不再补。 */
export const UPDATE_RESUME_CHECK_MS = 5 * 60 * 1000;

/**
 * shouldCheckOnResume 判断回前台时是否补一次检查。
 *
 * 从未查过（null）立即补查；恰好间隔 UPDATE_RESUME_CHECK_MS 视为已到期。
 */
export function shouldCheckOnResume(lastCheckedAt: number | null, now: number): boolean {
	if (lastCheckedAt === null) return true;
	return now - lastCheckedAt >= UPDATE_RESUME_CHECK_MS;
}
