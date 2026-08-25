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
		delete window.__DEPLOYCONFIG;
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
		window.__DEPLOYCONFIG = { appName: "AcmeAI", logo: "./brand/logo.svg" };
		expect(readBrandName()).toBe("AcmeAI");
		expect(readBrandLogo()).toBe("./brand/logo.svg");
		expect(saveBrandName("LocalName")).toBe("LocalName");
		expect(readBrandName()).toBe("LocalName");
		expect(saveBrandLogo("file_logo_2")).toBe("file_logo_2");
		expect(readBrandLogo()).toBe("file_logo_2");
		expect(saveBrandName("   ")).toBe(DEFAULT_BRAND_NAME);
		expect(readBrandName()).toBe("AcmeAI");
		expect(readCustomBrandName()).toBeNull();
	});

	it("uses packed deploy config when localStorage is empty", () => {
		window.__DEPLOYCONFIG = { appName: "AcmeAI", logo: "./brand/logo.svg" };
		expect(readBrandName()).toBe("AcmeAI");
		expect(readBrandLogo()).toBe("./brand/logo.svg");
		expect(readCustomBrandName()).toBeNull();
	});

	it("lets localStorage override packed deploy config", () => {
		window.__DEPLOYCONFIG = { appName: "AcmeAI", logo: "./brand/logo.svg" };
		expect(saveBrandName("LocalName")).toBe("LocalName");
		expect(saveBrandLogo("file_logo_2")).toBe("file_logo_2");
		expect(readBrandName()).toBe("LocalName");
		expect(readBrandLogo()).toBe("file_logo_2");
	});
});
