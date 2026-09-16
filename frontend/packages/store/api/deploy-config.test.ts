import { afterEach, describe, expect, it } from "vitest";
import {
	DEFAULT_DEPLOY_CONFIG,
	readDeployAppName,
	readDeployConfig,
	readDeployLogo,
	readXidianDemoLogin,
	resolveXidianDemoLogin,
	XIDIAN_DEMO_LOGIN,
} from "./deploy-config";

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

	it("prefills xidian demo login only for private xidian mode", () => {
		expect(resolveXidianDemoLogin(false, "xidian")).toBeNull();
		expect(resolveXidianDemoLogin(true, "acme")).toBeNull();
		expect(resolveXidianDemoLogin(true, "Xidian")).toEqual({
			account: XIDIAN_DEMO_LOGIN.account,
			password: XIDIAN_DEMO_LOGIN.password,
		});
	});

	it("reads xidian demo login from packed deploy config", () => {
		window.__DEPLOYCONFIG = { version: "private", mode: "xidian", appName: "Lework", logo: "" };
		expect(readXidianDemoLogin(true)).toEqual({
			account: "abc@xidian.com",
			password: "abc123456",
		});
		expect(readXidianDemoLogin(false)).toBeNull();
	});
});
