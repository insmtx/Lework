import { type ApiClient, createApiClient } from "@lework/api";

import { notifySessionExpired } from "../settings/session-events";
import type { Settings } from "../settings/settings";

// createClient 每次请求都从 settings 现取令牌：令牌在设置页更新后，
// 已创建的 client 不需要重建就能用上新值。
//
// onExpired 默认是全局通知器：调用点不必逐个接线，401 也会被外壳统一处理。
export function createClient(
	settings: Settings,
	onExpired: () => void = notifySessionExpired,
): ApiClient {
	return createApiClient({
		baseUrl: settings.baseUrl,
		headers: (): Record<string, string> =>
			settings.sessionToken ? { Authorization: `Bearer ${settings.sessionToken}` } : {},
		onUnauthorized: (error) => {
			// 401 有两种含义：会话真的过期（unauthorized），或提交的凭证字段本身
			// 就错了（invalid_credentials，典型是改密码时旧密码输错）。后者不能
			// 清本地会话，否则用户正在改的密码会被自己的笔误踢回登录页。
			if (error.code === "invalid_credentials") return;
			onExpired();
		},
	});
}
