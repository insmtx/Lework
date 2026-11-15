// 设置保存「连到哪个控制面、以谁的身份、要不要推送」，不保存任何会话事实。
export interface Settings {
	baseUrl: string;
	/** 登录账号；空串表示未登录。 */
	email: string;
	/**
	 * 会话令牌。只存 SecureStore，通过 Authorization 头发送；
	 * 它不是部署级 enrollment token，换账号/换服务器都必须丢弃。
	 */
	sessionToken: string;
	notifications: boolean;
	deviceID?: string;
}

export interface KeyValueStore {
	getItem(key: string): Promise<string | null>;
	setItem(key: string, value: string): Promise<void>;
	deleteItem(key: string): Promise<void>;
}

export interface SettingsStore {
	load(): Promise<Settings | null>;
	save(settings: Settings): Promise<void>;
	clear(): Promise<void>;
}

export const SETTINGS_KEY = "lework.settings.v1";

// normalizeBaseUrl 把用户输入收敛成可拼接的基址：去掉空白与末尾斜杠，
// 只接受 http(s)，并拒绝没有协议的输入——移动端没有“同源”可依赖。
export function normalizeBaseUrl(raw: string): string {
	const trimmed = raw.trim();
	if (!trimmed) throw new Error("server address is required");

	let url: URL;
	try {
		url = new URL(trimmed);
	} catch {
		throw new Error("server address must be a full URL, e.g. https://box:8787");
	}
	if (url.protocol !== "http:" && url.protocol !== "https:") {
		throw new Error("server address must start with http:// or https://");
	}
	url.hash = "";
	url.search = "";
	return url.toString().replace(/\/+$/, "");
}

// createSettingsStore 把 JSON 序列化与校验和具体存储解耦，
// 这样 SecureStore 只在运行时接线，纯逻辑可以直接单测。
export function createSettingsStore(kv: KeyValueStore): SettingsStore {
	return {
		async load() {
			const raw = await kv.getItem(SETTINGS_KEY);
			if (!raw) return null;
			try {
				const parsed = JSON.parse(raw) as Partial<Settings> & { token?: unknown };
				if (typeof parsed.baseUrl !== "string" || typeof parsed.notifications !== "boolean") {
					return null;
				}
				// 旧版本存的是 baseUrl + token（部署级令牌）。令牌不该变成个人
				// 会话，也不能继续留在手机上：丢弃它，只保留地址，让用户登录。
				if (typeof parsed.sessionToken !== "string") {
					return {
						baseUrl: parsed.baseUrl,
						email: "",
						sessionToken: "",
						notifications: parsed.notifications,
						deviceID: typeof parsed.deviceID === "string" ? parsed.deviceID : undefined,
					};
				}
				return {
					baseUrl: parsed.baseUrl,
					email: typeof parsed.email === "string" ? parsed.email : "",
					sessionToken: parsed.sessionToken,
					notifications: parsed.notifications,
					deviceID: typeof parsed.deviceID === "string" ? parsed.deviceID : undefined,
				};
			} catch {
				return null;
			}
		},

		save(settings) {
			return kv.setItem(SETTINGS_KEY, JSON.stringify(settings));
		},

		clear() {
			return kv.deleteItem(SETTINGS_KEY);
		},
	};
}
