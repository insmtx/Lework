import { type ApiClient, createApiClient } from "@lework/api";

import type { Settings } from "../settings/settings";

// createClient 每次请求都从 settings 现取令牌：令牌在设置页更新后，
// 已创建的 client 不需要重建就能用上新值。
export function createClient(settings: Settings): ApiClient {
	return createApiClient({
		baseUrl: settings.baseUrl,
		headers: () => ({ Authorization: `Bearer ${settings.token}` }),
	});
}
