export type DeployVersion = "public" | "private";

export type DeployConfig = {
	version: DeployVersion;
	mode: string;
	appName: string;
	logo: string;
};

export const DEFAULT_DEPLOY_APP_NAME = "Lework";

export const DEFAULT_DEPLOY_CONFIG: DeployConfig = {
	version: "public",
	mode: "",
	appName: DEFAULT_DEPLOY_APP_NAME,
	logo: "",
};

declare global {
	interface Window {
		__DEPLOYCONFIG?: Partial<DeployConfig>;
	}
}

function readWindowDeployConfig(): Partial<DeployConfig> | null {
	if (typeof window === "undefined") return null;
	const config = window.__DEPLOYCONFIG;
	if (!config || typeof config !== "object") return null;
	return config;
}

function trimString(value: unknown): string {
	return typeof value === "string" ? value.trim() : "";
}

/** 读取打包打入的部署配置；缺失字段回退到 SaaS 默认值。 */
export function readDeployConfig(): DeployConfig {
	const raw = readWindowDeployConfig();
	if (!raw) return { ...DEFAULT_DEPLOY_CONFIG };

	const version = raw.version === "private" ? "private" : "public";
	const appName = trimString(raw.appName) || DEFAULT_DEPLOY_APP_NAME;

	return {
		version,
		mode: trimString(raw.mode),
		appName,
		logo: trimString(raw.logo),
	};
}

/** 打包注入的 Logo 路径；未注入时返回 null。 */
export function readDeployLogo(): string | null {
	const logo = readDeployConfig().logo;
	return logo || null;
}

/** 打包注入的品牌名；未单独注入时返回 null，避免把默认 Lework 当成注入值。 */
export function readDeployAppName(): string | null {
	const raw = readWindowDeployConfig();
	const appName = trimString(raw?.appName);
	if (!appName || appName === DEFAULT_DEPLOY_APP_NAME) return null;
	return appName;
}
