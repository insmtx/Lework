import { ApiError } from "@lework/api";

import { normalizeBaseUrl, type Settings, type SettingsStore } from "./settings";

/** AuthClient 是会话逻辑需要的最小客户端面；从 @lework/api 的 ApiClient 投影而来。 */
export interface AuthClient {
	getAuthMethods(): Promise<{ initialized: boolean; registration_mode: "open" | "closed" }>;
	login(email: string, password: string): Promise<{ token: string }>;
	register(email: string, password: string): Promise<{ token: string }>;
	logout(): Promise<unknown>;
	changePassword(currentPassword: string, newPassword: string): Promise<unknown>;
}

export interface SessionDeps {
	store: SettingsStore;
	/** client 按地址（可选带令牌）构造；登录用无令牌的，登出用带令牌的。 */
	client: (baseUrl: string, token?: string) => AuthClient;
}

/** 登录：成功后把地址、账号与会话令牌一起落盘。 */
export async function signIn(
	deps: SessionDeps,
	input: { baseUrl: string; email: string; password: string },
): Promise<Settings> {
	const trimmed = input.email.trim();
	if (!trimmed) throw new Error("请输入账号");
	const baseUrl = normalizeBaseUrl(input.baseUrl);
	const result = await withAuthError(() => deps.client(baseUrl).login(trimmed, input.password));
	const previous = await deps.store.load();
	return saveSession(deps, previous, {
		baseUrl,
		email: trimmed,
		sessionToken: result.token,
	});
}

/** 注册：后端注册即登录，本地处理与登录完全一致。 */
export async function signUp(
	deps: SessionDeps,
	input: { baseUrl: string; email: string; password: string },
): Promise<Settings> {
	const trimmed = input.email.trim();
	if (!trimmed) throw new Error("请输入账号");
	const baseUrl = normalizeBaseUrl(input.baseUrl);
	const result = await withAuthError(() => deps.client(baseUrl).register(trimmed, input.password));
	const previous = await deps.store.load();
	return saveSession(deps, previous, {
		baseUrl,
		email: trimmed,
		sessionToken: result.token,
	});
}

/** 登出：先尽力吊销服务端会话，无论成败都清本地登录态（保留地址）。 */
export async function signOut(deps: SessionDeps, settings: Settings): Promise<Settings> {
	try {
		await deps.client(settings.baseUrl, settings.sessionToken).logout();
	} catch {
		// 令牌可能已经失效；本地清理才是用户要的结果。
	}
	return saveSession(deps, settings, { baseUrl: settings.baseUrl, email: "", sessionToken: "" });
}

/** clearExpiredSession 用在任何接口返回 401 之后：只清登录态，不动地址。 */
export async function clearExpiredSession(
	store: SettingsStore,
	settings: Settings,
): Promise<Settings> {
	return saveSession({ store, client: () => unreachableAuthClient() }, settings, {
		baseUrl: settings.baseUrl,
		email: "",
		sessionToken: "",
	});
}

/** changePassword 只调用接口，不改本地会话（当前会话在后端被保留）。 */
export async function changePassword(
	deps: SessionDeps,
	settings: Settings,
	input: { currentPassword: string; newPassword: string },
): Promise<void> {
	await deps.client(settings.baseUrl, settings.sessionToken).changePassword(
		input.currentPassword,
		input.newPassword,
	);
}

async function withAuthError<T>(call: () => Promise<T>): Promise<T> {
	try {
		return await call();
	} catch (error) {
		// 账号密码错误在 UI 上必须是人话，不能让 ApiError 直接冒到页面。
		throw new Error(authErrorMessage(error));
	}
}

async function saveSession(
	deps: SessionDeps,
	previous: Settings | null,
	next: { baseUrl: string; email: string; sessionToken: string },
): Promise<Settings> {
	const saved: Settings = {
		baseUrl: next.baseUrl,
		email: next.email,
		sessionToken: next.sessionToken,
		notifications: previous?.notifications ?? true,
		// 换服务器/换账号时设备登记必须重来：设备 ID 只属于一个控制面。
		deviceID:
			previous && previous.baseUrl === next.baseUrl && previous.email === next.email
				? previous.deviceID
				: undefined,
	};
	await deps.store.save(saved);
	return saved;
}

/** authErrorMessage 把后端错误码/网络故障翻译成用户可照做的提示。 */
export function authErrorMessage(error: unknown): string {
	if (error instanceof ApiError) {
		switch (error.code) {
			case "invalid_credentials":
				return "账号或密码错误";
			case "registration_closed":
				return "这台服务器未开放注册";
			case "too_many_attempts":
				return "尝试次数过多，请稍后再试";
			case "invalid_request":
				return "账号或密码不符合要求（密码至少 8 位）";
			default:
				if (error.status === 401) return "账号或密码错误";
				if (error.status === 429) return "尝试次数过多，请稍后再试";
				return "操作失败，请稍后重试";
		}
	}
	return "网络不通，请检查服务器地址";
}

function unreachableAuthClient(): AuthClient {
	throw new Error("session cleanup must not perform network calls");
}
