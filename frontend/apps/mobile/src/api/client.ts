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
		headers: () =>
			settings.sessionToken ? { Authorization: `Bearer ${settings.sessionToken}` } : {},
		onUnauthorized: () => onExpired(),
	});
}
