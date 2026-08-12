type PublicEnv = {
	readonly NEXT_PUBLIC_LEROS_API_BASE_URL?: string;
	readonly VITE_LEROS_API_BASE_URL?: string;
	readonly VITE_LEROS_DEPLOYMENT_MODE?: string;
	readonly LEROS_API_BASE_URL?: string;
};

declare const process:
	| {
			readonly env?: PublicEnv;
	  }
	| undefined;

const DEFAULT_API_BASE_URL = "http://localhost:8080/v1";
export const SERVER_CONFIG_STORAGE_KEY = "leros-server-base-url";
export const PRIVATE_SERVER_CONFIG_STORAGE_KEY = "leros-private-server-base-url";
/** 本地调试用：存在该 key 时强制走私有化模式，无需打包 private 构建。 */
export const PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY = "leros-private-deployment-mode";
const PRIVATE_DEPLOYMENT_MODE = "private";
const CONNECTION_TEST_TIMEOUT_MS = 8000;

function getNextAPIBaseURL(): string | undefined {
	if (typeof process === "undefined") return undefined;
	return process.env?.NEXT_PUBLIC_LEROS_API_BASE_URL || process.env?.LEROS_API_BASE_URL;
}

function getViteAPIBaseURL(): string | undefined {
	return (import.meta as ImportMeta & { readonly env?: PublicEnv }).env?.VITE_LEROS_API_BASE_URL;
}

function getViteDeploymentMode(): string | undefined {
	return (import.meta as ImportMeta & { readonly env?: PublicEnv }).env?.VITE_LEROS_DEPLOYMENT_MODE;
}

function hasPrivateDeploymentModeOverride(): boolean {
	if (typeof window === "undefined") return false;

	try {
		return window.localStorage.getItem(PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY) !== null;
	} catch {
		return false;
	}
}

/**
 * 是否私有化客户端。
 * 优先看 localStorage 覆盖开关；未设置时回退到构建环境变量 VITE_LEROS_DEPLOYMENT_MODE。
 * 通过 DevTools 增删覆盖项后需刷新页面才会生效。
 */
export function resolveIsPrivateDeployment(): boolean {
	return hasPrivateDeploymentModeOverride() || getViteDeploymentMode() === PRIVATE_DEPLOYMENT_MODE;
}

export const isPrivateDeployment = resolveIsPrivateDeployment();

export function normalizeAPIBaseURL(value: string): string {
	const trimmed = value.trim();
	if (!trimmed) {
		throw new Error("请输入后端服务地址");
	}

	let url: URL;
	try {
		url = new URL(trimmed);
	} catch {
		throw new Error("服务地址格式无效，请输入完整的 http:// 或 https:// 地址");
	}

	if (url.protocol !== "http:" && url.protocol !== "https:") {
		throw new Error("服务地址仅支持 http:// 或 https://");
	}
	if (url.username || url.password) {
		throw new Error("服务地址不能包含用户名或密码");
	}
	if (url.search || url.hash) {
		throw new Error("服务地址不能包含查询参数或锚点");
	}

	const pathname = url.pathname.replace(/\/+$/, "");
	url.pathname = pathname.endsWith("/v1") ? pathname : `${pathname}/v1`;

	return url.toString().replace(/\/+$/, "");
}

export function readPrivateServerBaseURL(): string | null {
	if (typeof window === "undefined") return null;

	try {
		const stored = window.localStorage.getItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY);
		return stored ? normalizeAPIBaseURL(stored) : null;
	} catch {
		return null;
	}
}

export function readServerBaseURL(): string | null {
	if (typeof window === "undefined") return null;

	try {
		const stored =
			window.localStorage.getItem(SERVER_CONFIG_STORAGE_KEY) ??
			window.localStorage.getItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY);
		return stored ? normalizeAPIBaseURL(stored) : null;
	} catch {
		return null;
	}
}

export function saveServerBaseURL(value: string): string {
	if (typeof window === "undefined") {
		throw new Error("当前环境不支持保存服务地址");
	}

	const normalized = normalizeAPIBaseURL(value);
	window.localStorage.setItem(SERVER_CONFIG_STORAGE_KEY, normalized);
	return normalized;
}

export function savePrivateServerBaseURL(value: string): string {
	if (typeof window === "undefined") {
		throw new Error("当前环境不支持保存服务地址");
	}

	const normalized = normalizeAPIBaseURL(value);
	window.localStorage.setItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY, normalized);
	window.localStorage.setItem(SERVER_CONFIG_STORAGE_KEY, normalized);
	return normalized;
}

export function hasPrivateServerConfiguration(): boolean {
	return readPrivateServerBaseURL() !== null;
}

export async function testServerConnection(value: string): Promise<string> {
	const normalized = normalizeAPIBaseURL(value);
	const controller = new AbortController();
	const timeoutId = window.setTimeout(() => controller.abort(), CONNECTION_TEST_TIMEOUT_MS);

	try {
		const response = await fetch(`${normalized}/GlobalConfig`, {
			method: "GET",
			headers: { Accept: "application/json" },
			signal: controller.signal,
		});
		if (!response.ok) {
			throw new Error(`服务返回异常状态（HTTP ${response.status}）`);
		}

		const payload: unknown = await response.json();
		if (!isGlobalConfigResponse(payload)) {
			throw new Error("服务响应格式不正确，请确认连接的是 Lework 后端");
		}
		return normalized;
	} catch (error) {
		if (error instanceof Error && error.name === "AbortError") {
			throw new Error("连接超时，请检查服务地址和网络");
		}
		if (error instanceof Error) {
			throw error;
		}
		throw new Error("无法连接后端服务");
	} finally {
		window.clearTimeout(timeoutId);
	}
}

function isGlobalConfigResponse(value: unknown): boolean {
	if (typeof value !== "object" || value === null || !("data" in value)) return false;
	const data = (value as { data?: unknown }).data;
	if (typeof data !== "object" || data === null || !("edition" in data)) return false;
	const edition = (data as { edition?: unknown }).edition;
	return edition === "oss" || edition === "enterprise";
}

function resolveAPIBaseURL(): string {
	const baseURL =
		readServerBaseURL() || getViteAPIBaseURL() || getNextAPIBaseURL() || DEFAULT_API_BASE_URL;

	return normalizeAPIBaseURL(baseURL);
}

export const API_BASE_URL = resolveAPIBaseURL();
