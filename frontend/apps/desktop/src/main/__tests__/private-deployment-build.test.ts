import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const desktopRoot = resolve(__dirname, "../../..");

describe("private deployment build marker", () => {
	it("injects the deployment mode into the renderer build", () => {
		const viteConfig = readFileSync(resolve(desktopRoot, "electron.vite.config.ts"), "utf8");

		expect(viteConfig).toContain('"import.meta.env.VITE_LEROS_DEPLOYMENT_MODE"');
		expect(viteConfig).toContain('process.env.VITE_LEROS_DEPLOYMENT_MODE ?? "public"');
	});

	it("provides dedicated private packaging commands", () => {
		const privateBuildScript = readFileSync(
			resolve(desktopRoot, "scripts/dist-private.mjs"),
			"utf8",
		);
		const privateTargetScript = readFileSync(
			resolve(desktopRoot, "scripts/dist-private-target.mjs"),
			"utf8",
		);
		const desktopPackage = JSON.parse(
			readFileSync(resolve(desktopRoot, "package.json"), "utf8"),
		) as { scripts?: Record<string, string> };

		expect(desktopPackage.scripts?.["dist:private"]).toBe("node scripts/dist-private.mjs");
		expect(desktopPackage.scripts?.["dist:private:win:x64"]).toBe(
			"node scripts/dist-private-target.mjs --win --x64",
		);
		expect(desktopPackage.scripts?.["dist:private:mac:arm64"]).toBe(
			"node scripts/dist-private-target.mjs --mac --arm64",
		);
		expect(desktopPackage.scripts?.["dist:private:linux:x64"]).toBe(
			"node scripts/dist-private-target.mjs --linux --x64",
		);
		expect(privateBuildScript).toContain('VITE_LEROS_DEPLOYMENT_MODE = "private"');
		expect(privateBuildScript).toContain('import("./dist-local.mjs")');
		expect(privateTargetScript).toContain('VITE_LEROS_DEPLOYMENT_MODE = "private"');
		expect(privateTargetScript).toContain("runDesktopDist(builderArgs)");
	});
});
