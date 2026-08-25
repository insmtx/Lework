import { readDeployAppName, readDeployLogo } from "./deploy-config";

/** 存在该 key 时，私有化系统设置展示品牌 Logo / 品牌名编辑区。 */
export const BRANDING_SETTINGS_ENABLED_STORAGE_KEY = "leros-branding-settings-enabled";
/** 自定义系统 Logo（通常为上传后的 file public_id）。 */
export const BRAND_LOGO_STORAGE_KEY = "leros-brand-logo";
/** 自定义系统品牌名。 */
export const BRAND_NAME_STORAGE_KEY = "leros-brand-name";

export const BRANDING_CHANGED_EVENT = "leros-branding-changed";

export const DEFAULT_BRAND_NAME = "Lework";

function notifyBrandingChanged() {
	if (typeof window === "undefined") return;
	window.dispatchEvent(new Event(BRANDING_CHANGED_EVENT));
}

function readStorageItem(key: string): string | null {
	if (typeof window === "undefined") return null;
	try {
		const value = window.localStorage.getItem(key);
		const trimmed = value?.trim();
		return trimmed ? trimmed : null;
	} catch {
		return null;
	}
}

function writeStorageItem(key: string, value: string | null) {
	if (typeof window === "undefined") {
		throw new Error("当前环境不支持保存品牌配置");
	}

	if (value === null) {
		window.localStorage.removeItem(key);
	} else {
		window.localStorage.setItem(key, value);
	}
	notifyBrandingChanged();
}

/** 是否展示品牌自定义编辑区（看 key 是否存在，不看 value）。 */
export function isBrandingSettingsEnabled(): boolean {
	if (typeof window === "undefined") return false;
	try {
		return window.localStorage.getItem(BRANDING_SETTINGS_ENABLED_STORAGE_KEY) !== null;
	} catch {
		return false;
	}
}

/** 读取自定义 Logo；优先 localStorage，其次打包注入，未配置时返回 null。 */
export function readBrandLogo(): string | null {
	return readStorageItem(BRAND_LOGO_STORAGE_KEY) ?? readDeployLogo();
}

export function saveBrandLogo(value: string): string {
	const trimmed = value.trim();
	if (!trimmed) {
		throw new Error("Logo 地址不能为空");
	}
	writeStorageItem(BRAND_LOGO_STORAGE_KEY, trimmed);
	return trimmed;
}

export function clearBrandLogo() {
	writeStorageItem(BRAND_LOGO_STORAGE_KEY, null);
}

/** 读取品牌名；优先 localStorage，其次打包注入的 appName，否则默认 Lework。 */
export function readBrandName(fallback: string = DEFAULT_BRAND_NAME): string {
	return readStorageItem(BRAND_NAME_STORAGE_KEY) ?? readDeployAppName() ?? fallback;
}

/** 读取已保存的自定义品牌名（不含默认回退），供设置页表单使用。 */
export function readCustomBrandName(): string | null {
	return readStorageItem(BRAND_NAME_STORAGE_KEY);
}

export function saveBrandName(value: string): string {
	const trimmed = value.trim();
	if (!trimmed) {
		writeStorageItem(BRAND_NAME_STORAGE_KEY, null);
		return DEFAULT_BRAND_NAME;
	}
	writeStorageItem(BRAND_NAME_STORAGE_KEY, trimmed);
	return trimmed;
}
