import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const desktopRoot = resolve(__dirname, "../../..");

describe("private deployment build marker", () => {
	it("injects the deployment mode into the renderer build", () => {
		const viteConfig = readFileSync(resolve(desktopRoot, "electron.vite.config.ts"), "utf8");

		expect(viteConfig).toContain('publicDir: resolve("src/renderer/public")');
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
		const distRunScript = readFileSync(resolve(desktopRoot, "scripts/dist-run.mjs"), "utf8");
		const desktopPackage = JSON.parse(
			readFileSync(resolve(desktopRoot, "package.json"), "utf8"),
		) as {
			scripts?: Record<string, string>;
			dependencies?: Record<string, string>;
			devDependencies?: Record<string, string>;
		};

		expect(desktopPackage.scripts?.["dist:private"]).toBe("node scripts/dist-private.mjs");
		expect(desktopPackage.scripts?.["dist:private:dir"]).toBe(
			"node scripts/dist-private-target.mjs --dir",
		);
		expect(desktopPackage.scripts?.["dist:private:win:x64"]).toBe(
			"node scripts/dist-private-target.mjs --win --x64",
		);
		expect(desktopPackage.scripts?.["dist:private:mac:arm64"]).toBe(
			"node scripts/dist-private-target.mjs --mac --arm64",
		);
		expect(desktopPackage.scripts?.["dist:private:linux:x64"]).toBe(
			"node scripts/dist-private-target.mjs --linux --x64",
		);
		expect(desktopPackage.scripts?.["dist:private:linux:arm64"]).toBe(
			"node scripts/dist-private-target.mjs --linux AppImage:arm64 deb:arm64",
		);
		expect(privateBuildScript).toContain('VITE_LEROS_DEPLOYMENT_MODE = "private"');
		expect(privateBuildScript).toContain('import("./dist-local.mjs")');
		expect(privateTargetScript).toContain('VITE_LEROS_DEPLOYMENT_MODE = "private"');
		expect(privateTargetScript).toContain("runDesktopDist(builderArgs)");
		expect(distRunScript).toContain("-linux-${arch}-private.${ext}");
		expect(desktopPackage.scripts?.icons).toContain("inject-deploy-config.mjs");
		expect(desktopPackage.scripts?.["dist:linux:arm64"]).toContain("AppImage:arm64 deb:arm64");
	});

	it("keeps private GitLab packages off the SaaS COS prefix", () => {
		const gitlabCi = readFileSync(resolve(desktopRoot, "../../../.gitlab-ci.yml"), "utf8");

		expect(gitlabCi).toContain("- private");
		expect(gitlabCi).toContain('if [ "$ENV" = "private" ]');
		expect(gitlabCi).toContain("dist:desktop${private_script_infix}");
		expect(gitlabCi).toContain('name_suffix="-private"');
		expect(gitlabCi).toContain("${root_prefix}/private/${deploy_mode}");
		expect(gitlabCi).toContain("Private desktop build requires LEROS_DEPLOY_MODE");
		expect(gitlabCi).toContain("Ignoring desktop_deploy_mode/desktop_app_name because environment=");
		expect(gitlabCi).toContain("Invalid LEROS_DEPLOY_MODE");
		expect(gitlabCi).toContain("/download?edition=private&mode=${deploy_mode}");
		expect(gitlabCi).toContain('$ENV != "private"');
		expect(gitlabCi).toContain("desktop-build-linux-arm64:");
		expect(gitlabCi).toContain('$APP == "desktop" && $ENV == "private"');
		expect(gitlabCi).toContain("dist:desktop${private_script_infix}:linux:arm64");
		expect(gitlabCi).toContain("Lework-${version}-linux-arm64${name_suffix}.deb");
	});

	it("keeps renderer workspace packages out of production dependencies", () => {
		const desktopPackage = JSON.parse(
			readFileSync(resolve(desktopRoot, "package.json"), "utf8"),
		) as {
			dependencies?: Record<string, string>;
			devDependencies?: Record<string, string>;
		};

		// 中文注释：renderer 依赖已被 Vite 打进 out/，若留在 dependencies，electron-builder
		// 会经 pnpm 展开整棵 workspace 树（含 @napi-rs/canvas 等），Windows 打包会卡很久。
		expect(Object.keys(desktopPackage.dependencies ?? {}).sort()).toEqual([
			"@electron-toolkit/preload",
			"@electron-toolkit/utils",
			"electron-updater",
		]);
		expect(desktopPackage.devDependencies?.["@leros/app-ui"]).toBe("workspace:*");
		expect(desktopPackage.devDependencies?.["@leros/store"]).toBe("workspace:*");
		expect(desktopPackage.devDependencies?.["@leros/ui"]).toBe("workspace:*");
		expect(desktopPackage.devDependencies?.react).toBe("catalog:");
	});
});