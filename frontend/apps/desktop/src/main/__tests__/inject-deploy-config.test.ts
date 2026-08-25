import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { injectDeployConfig, sanitizeDeployMode } from "../../../scripts/inject-deploy-config.mjs";

describe("inject deploy config", () => {
	const dirs: string[] = [];

	afterEach(async () => {
		await Promise.all(dirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
		vi.unstubAllEnvs();
	});

	it("rejects unsafe mode values", () => {
		expect(() => sanitizeDeployMode("../x")).toThrow(/Invalid LEROS_DEPLOY_MODE/);
		expect(() => sanitizeDeployMode("acme/h3c")).toThrow(/Invalid LEROS_DEPLOY_MODE/);
		expect(sanitizeDeployMode("acme")).toBe("acme");
	});

	it("keeps default logo when mode directory is missing", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "",
			deploymentMode: "private",
			frontendRoot: root,
			rendererPublicDir: join(root, "public"),
		});
		expect(result.logo).toBe("");
		expect(result.appName).toBe("Lework");
		expect(result.mode).toBe("acme");
		expect(result.version).toBe("private");
	});

	it("copies logo when the mode directory contains a logo file", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const modeDir = join(root, "private/logo/acme");
		await mkdir(modeDir, { recursive: true });
		await writeFile(join(modeDir, "logo.svg"), "<svg></svg>");
		const publicDir = join(root, "public");
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "AcmeAI",
			deploymentMode: "private",
			frontendRoot: root,
			rendererPublicDir: publicDir,
		});
		expect(result.logo).toBe("./brand/logo.svg");
		expect(result.appName).toBe("AcmeAI");
		const packed = await readFile(join(publicDir, "brand/logo.svg"), "utf8");
		expect(packed).toContain("<svg");
	});

	it("ignores mode and appName for public/SaaS packages", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const modeDir = join(root, "private/logo/acme");
		await mkdir(modeDir, { recursive: true });
		await writeFile(join(modeDir, "logo.svg"), "<svg></svg>");
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "AcmeAI",
			deploymentMode: "public",
			frontendRoot: root,
			rendererPublicDir: join(root, "public"),
		});
		expect(result.version).toBe("public");
		expect(result.mode).toBe("");
		expect(result.appName).toBe("Lework");
		expect(result.logo).toBe("");
	});

	it("fails when the mode directory exists but has no logo file", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		await mkdir(join(root, "private/logo/acme"), { recursive: true });
		await expect(
			injectDeployConfig({
				mode: "acme",
				deploymentMode: "private",
				frontendRoot: root,
				rendererPublicDir: join(root, "public"),
			}),
		).rejects.toThrow(/exists but has no logo.svg or logo.png/);
	});
});
