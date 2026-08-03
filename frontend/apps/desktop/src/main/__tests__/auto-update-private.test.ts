import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const desktopRoot = resolve(__dirname, "../../..");

describe("private deployment auto update policy", () => {
	it("injects deployment mode into the main process build", () => {
		const viteConfig = readFileSync(resolve(desktopRoot, "electron.vite.config.ts"), "utf8");

		expect(viteConfig).toContain("main: {");
		expect(viteConfig).toContain('"process.env.LEROS_DEPLOYMENT_MODE"');
	});

	it("disables auto update scheduling for private builds", () => {
		const autoUpdate = readFileSync(resolve(desktopRoot, "src/main/auto-update.ts"), "utf8");

		expect(autoUpdate).toContain('process.env.LEROS_DEPLOYMENT_MODE === "private"');
		expect(autoUpdate).toContain("私有化版本请通过离线安装包更新");
		expect(autoUpdate).toMatch(/registerDesktopAutoUpdate[\s\S]*isPrivateDeploymentBuild\(\)/);
	});
});
