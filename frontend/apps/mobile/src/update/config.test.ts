import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const appUrl = new URL("../../app.json", import.meta.url);
const pkgUrl = new URL("../../package.json", import.meta.url);

describe("mobile update app config", () => {
	it("requests install packages permission", () => {
		const app = JSON.parse(readFileSync(appUrl, "utf8"));
		expect(app.expo.android.permissions).toContain("android.permission.REQUEST_INSTALL_PACKAGES");
	});

	it("declares the expo modules used by the updater", () => {
		const pkg = JSON.parse(readFileSync(pkgUrl, "utf8"));
		expect(pkg.dependencies["expo-application"]).toBeTruthy();
		expect(pkg.dependencies["expo-file-system"]).toBeTruthy();
		expect(pkg.dependencies["expo-intent-launcher"]).toBeTruthy();
	});
});
