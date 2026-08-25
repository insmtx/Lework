import { afterEach, describe, expect, it } from "vitest";
import { DEFAULT_DEPLOY_CONFIG, readDeployAppName, readDeployConfig, readDeployLogo } from "./deploy-config";

describe("deploy config", () => {
	afterEach(() => {
		delete window.__DEPLOYCONFIG;
	});

	it("returns SaaS defaults when window config is missing", () => {
		expect(readDeployConfig()).toEqual(DEFAULT_DEPLOY_CONFIG);
		expect(readDeployLogo()).toBeNull();
		expect(readDeployAppName()).toBeNull();
	});

	it("reads packed private branding", () => {
		window.__DEPLOYCONFIG = {
			version: "private",
			mode: "acme",
			appName: "AcmeAI",
			logo: "./brand/logo.svg",
		};
		expect(readDeployConfig()).toEqual({
			version: "private",
			mode: "acme",
			appName: "AcmeAI",
			logo: "./brand/logo.svg",
		});
		expect(readDeployLogo()).toBe("./brand/logo.svg");
		expect(readDeployAppName()).toBe("AcmeAI");
	});

	it("does not treat default Lework appName as an injected brand", () => {
		window.__DEPLOYCONFIG = { version: "private", mode: "acme", appName: "Lework", logo: "" };
		expect(readDeployAppName()).toBeNull();
		expect(readDeployLogo()).toBeNull();
	});
});
