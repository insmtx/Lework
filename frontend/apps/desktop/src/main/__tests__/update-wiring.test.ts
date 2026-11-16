import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(__dirname, "../../..");

function read(relativePath: string): string {
	return readFileSync(resolve(root, relativePath), "utf8");
}

describe("desktop update wiring", () => {
	it("reads LATEST.txt before asking electron-updater", () => {
		const source = read("src/main/auto-update.ts");
		expect(source).toContain("latestTxtUrl");
		expect(source).toContain("setFeedURL");
		expect(source).not.toContain("applework.insmtx.com");
	});

	it("keeps private deployments disabled", () => {
		const source = read("src/main/auto-update.ts");
		expect(source).toContain('process.env.LEWORK_DEPLOYMENT_MODE === "private"');
	});

	it("keeps electron-builder publish feed on the COS application layout", () => {
		const builder = read("electron-builder.yml");
		expect(builder).toContain("cos.ap-beijing.myqcloud.com/application");
		expect(builder).not.toContain("application/stable");
	});

	it("keeps the dev update config on the COS application layout", () => {
		const devConfig = read("dev-app-update.yml");
		expect(devConfig).toContain("cos.ap-beijing.myqcloud.com/application");
		expect(devConfig).not.toContain("application/stable");
	});
});
