import { afterEach, describe, expect, it } from "vitest";
import {
	BRAND_LOGO_STORAGE_KEY,
	BRAND_NAME_STORAGE_KEY,
	BRANDING_SETTINGS_ENABLED_STORAGE_KEY,
	clearBrandLogo,
	DEFAULT_BRAND_NAME,
	isBrandingSettingsEnabled,
	readBrandLogo,
	readBrandName,
	readCustomBrandName,
	saveBrandLogo,
	saveBrandName,
} from "./branding";

describe("branding storage", () => {
	afterEach(() => {
		window.localStorage.removeItem(BRANDING_SETTINGS_ENABLED_STORAGE_KEY);
		window.localStorage.removeItem(BRAND_LOGO_STORAGE_KEY);
		window.localStorage.removeItem(BRAND_NAME_STORAGE_KEY);
	});

	it("treats branding settings as disabled until the enable key exists", () => {
		expect(isBrandingSettingsEnabled()).toBe(false);
		window.localStorage.setItem(BRANDING_SETTINGS_ENABLED_STORAGE_KEY, "1");
		expect(isBrandingSettingsEnabled()).toBe(true);
	});

	it("reads and writes brand logo", () => {
		expect(readBrandLogo()).toBeNull();
		expect(saveBrandLogo("file_logo_1")).toBe("file_logo_1");
		expect(readBrandLogo()).toBe("file_logo_1");
		clearBrandLogo();
		expect(readBrandLogo()).toBeNull();
	});

	it("falls back to default brand name when unset or cleared", () => {
		expect(readBrandName()).toBe(DEFAULT_BRAND_NAME);
		expect(readCustomBrandName()).toBeNull();
		expect(saveBrandName("Acme")).toBe("Acme");
		expect(readBrandName()).toBe("Acme");
		expect(readCustomBrandName()).toBe("Acme");
		expect(saveBrandName("   ")).toBe(DEFAULT_BRAND_NAME);
		expect(readBrandName()).toBe(DEFAULT_BRAND_NAME);
		expect(readCustomBrandName()).toBeNull();
	});
});
