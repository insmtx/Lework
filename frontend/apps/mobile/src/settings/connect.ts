import type { PairPayload } from "../pairing/link";
import { normalizeBaseUrl, type Settings } from "./settings";

// ConnectDeps 把网络与存储注入进来，便于单测验证调用顺序。
export interface ConnectDeps {
	// verify 用候选配置请求一次控制面，确认地址与令牌可用。
	verify: (settings: Settings) => Promise<unknown>;
	// save 只在验证通过后调用，避免覆盖已经能用的旧配置。
	save: (settings: Settings) => Promise<void>;
	// notifications 沿用用户原有偏好，没有旧配置时默认开启。
	notifications: boolean;
}

// connectToControlPlane 先验证再落盘。
//
// 顺序不能反：扫码可能扫到过期的码或别的控制面地址，先写盘再验证会让
// App 停在“已经连接但用不了”的状态，连不回原来的配置。
export async function connectToControlPlane(
	payload: PairPayload,
	deps: ConnectDeps,
): Promise<Settings> {
	const settings: Settings = {
		baseUrl: payload.baseUrl,
		token: payload.token,
		notifications: deps.notifications,
	};
	await deps.verify(settings);
	await deps.save(settings);
	return settings;
}

/** ConnectionDraft 是服务器设置页里尚未校验的表单值。 */
export interface ConnectionDraft {
	baseUrl: string;
	token: string;
	notifications: boolean;
	deviceID?: string;
}

/**
 * buildConnectionSettings 把表单值收敛成可落盘的设置。
 *
 * 令牌必须存在：没有令牌的「已连接」只会在第一次请求时失败，不如保存前拒绝。
 * 地址校验复用 normalizeBaseUrl，保证存储里的地址永远可以直接拼路径。
 */
export function buildConnectionSettings(draft: ConnectionDraft): Settings {
	const token = draft.token.trim();
	if (!token) throw new Error("token is required");
	return {
		baseUrl: normalizeBaseUrl(draft.baseUrl),
		token,
		notifications: draft.notifications,
		deviceID: draft.deviceID,
	};
}

/**
 * connectionChanged 判断地址或令牌是否变了。
 *
 * 只有换了控制面才需要重新登记推送设备：设备 ID 属于某台控制面，
 * 单纯开关通知不应该被当成换服务器。
 */
export function connectionChanged(previous: Settings | null, next: Settings): boolean {
	if (!previous) return true;
	return previous.baseUrl !== next.baseUrl || previous.token !== next.token;
}
