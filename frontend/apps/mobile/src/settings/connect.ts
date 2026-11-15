import type { PairPayload } from "../pairing/link";
import type { Settings } from "./settings";

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
