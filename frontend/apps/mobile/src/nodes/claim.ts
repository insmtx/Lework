import { ApiError } from "@lework/api";

/** normalizeCodeInput 让手输框与服务端短码对齐。 */
export function normalizeCodeInput(raw: string): string {
	return raw.toUpperCase().replace(/[^0-9A-Z]/g, "");
}

/** isCompleteCode 决定「输满自动预览」的触发点。 */
export function isCompleteCode(code: string): boolean {
	return /^[0-9A-Z]{4,8}$/.test(code);
}

/**
 * claimExpiryLabel 把 expires_at 变成剩余时间文案。
 *
 * 只到「分 / 秒」：认领码默认 5 分钟，秒级足够；返回固定文案而不是负数，
 * 因为用户看到的「已失效」比「-30 秒后失效」可执行得多。
 */
export function claimExpiryLabel(expiresAt: string, now: number = Date.now()): string {
	const remain = Date.parse(expiresAt) - now;
	if (!Number.isFinite(remain) || remain <= 0) return "已失效，请让那台机器重新出码";
	const totalSeconds = Math.floor(remain / 1000);
	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;
	if (minutes === 0) return `${seconds} 秒后失效`;
	return `${minutes} 分 ${seconds} 秒后失效`;
}

/** describeClaimError 把认领接口的失败翻译成可照做的下一步。 */
export function describeClaimError(error: unknown): string {
	if (error instanceof ApiError) {
		switch (error.code) {
			case "claim_not_found":
				return "码无效或已过期，请让那台机器重新出码";
			case "too_many_attempts":
				return "尝试次数过多，请稍后再试";
			default:
				if (error.status === 401) return "登录已过期，请重新登录";
				if (error.status === 429) return "尝试次数过多，请稍后再试";
				return "添加失败，请稍后重试";
		}
	}
	return "网络不通，请检查服务器地址";
}
