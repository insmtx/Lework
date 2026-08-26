import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	injectDeployConfig,
	sanitizeDeployMode,
	sanitizeS3Domain,
} from "../../../scripts/inject-deploy-config.mjs";

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

	it("rejects unsafe S3 domains", () => {
		expect(() => sanitizeS3Domain("not-a-url")).toThrow(/Invalid LEROS_DEPLOY_S3_DOMAIN/);
		expect(() => sanitizeS3Domain("javascript:alert(1)")).toThrow(/Invalid LEROS_DEPLOY_S3_DOMAIN/);
		expect(sanitizeS3Domain("https://leros-1395325824.cos.ap-beijing.myqcloud.com/")).toBe(
			"https://leros-1395325824.cos.ap-beijing.myqcloud.com",
		);
	});

	it("keeps default logo when S3 domain is missing", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "",
			s3Domain: "",
			deploymentMode: "private",
			rendererPublicDir: join(root, "public"),
		});
		expect(result.logo).toBe("");
		expect(result.appName).toBe("Lework");
		expect(result.mode).toBe("acme");
		expect(result.version).toBe("private");
	});

	it("downloads logo from S3 domain /fronted/{mode}/", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const publicDir = join(root, "public");
		const fetchImpl = vi.fn(async (url: string) => {
			if (String(url).endsWith("/fronted/acme/logo.svg")) {
				return new Response("<svg id='remote'></svg>", { status: 200 });
			}
			return new Response("", { status: 404 });
		});
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "AcmeAI",
			s3Domain: "https://leros-1395325824.cos.ap-beijing.myqcloud.com",
			deploymentMode: "private",
			rendererPublicDir: publicDir,
			fetchImpl,
		});
		expect(result.logo).toBe("./brand/logo.svg");
		expect(fetchImpl).toHaveBeenCalledWith(
			"https://leros-1395325824.cos.ap-beijing.myqcloud.com/fronted/acme/logo.svg",
		);
		const packed = await readFile(join(publicDir, "brand/logo.svg"), "utf8");
		expect(packed).toContain("remote");
	});

	it("fails when S3 domain is set without mode", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		await expect(
			injectDeployConfig({
				mode: "",
				s3Domain: "https://cdn.example.com",
				deploymentMode: "private",
				rendererPublicDir: join(root, "public"),
			}),
		).rejects.toThrow(/requires LEROS_DEPLOY_MODE/);
	});

	it("fails when S3 objects are missing", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		await expect(
			injectDeployConfig({
				mode: "acme",
				appName: "AcmeAI",
				s3Domain: "https://cdn.example.com",
				deploymentMode: "private",
				rendererPublicDir: join(root, "public"),
				fetchImpl: async () => new Response("", { status: 404 }),
			}),
		).rejects.toThrow(/未找到定制 Logo/);
	});

	it("ignores mode, appName and S3 domain for public/SaaS packages", async () => {
		const root = await mkdtemp(join(tmpdir(), "leros-deploy-"));
		dirs.push(root);
		const fetchImpl = vi.fn();
		const result = await injectDeployConfig({
			mode: "acme",
			appName: "AcmeAI",
			s3Domain: "https://cdn.example.com",
			deploymentMode: "public",
			rendererPublicDir: join(root, "public"),
			fetchImpl,
		});
		expect(result.version).toBe("public");
		expect(result.mode).toBe("");
		expect(result.appName).toBe("Lework");
		expect(result.logo).toBe("");
		expect(fetchImpl).not.toHaveBeenCalled();
	});
});
