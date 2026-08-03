import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const builderConfigPath = resolve(__dirname, "../../../electron-builder.yml");
const desktopPackagePath = resolve(__dirname, "../../../package.json");

describe("electron-builder 桌面图标与资源配置", () => {
	it("不应禁用 Windows 可执行文件资源编辑，否则安装后的图标会回退为默认值", () => {
		const config = readFileSync(builderConfigPath, "utf8");

		// 中文注释：Windows 的 EXE 图标资源写入依赖 signAndEditExecutable，显式关闭会导致快捷方式和任务栏图标丢失。
		expect(config).not.toMatch(/signAndEditExecutable:\s*false/);
	});

	it("macOS 应使用带底板边距的专用图标，避免 Dock 中透明大图显得过大", () => {
		const config = readFileSync(builderConfigPath, "utf8");

		expect(config).toMatch(/mac:\s*\n\s*icon:\s*resources\/icon-mac\.png/);
	});

	it("Windows 应继续使用 ICO 资源", () => {
		const config = readFileSync(builderConfigPath, "utf8");

		expect(config).toMatch(/win:\s*\n\s*icon:\s*resources\/icon\.ico/);
	});

	it("Linux 应同步 desktopName，确保任务栏窗口能关联应用图标", () => {
		const config = readFileSync(builderConfigPath, "utf8");
		const desktopPackage = JSON.parse(readFileSync(desktopPackagePath, "utf8")) as {
			desktopName?: string;
		};

		expect(desktopPackage.desktopName).toBe("leros-desktop.desktop");
		expect(config).toMatch(
			/linux:[\s\S]*icon:\s*resources\/linux-icons[\s\S]*executableName:\s*leros-desktop[\s\S]*syncDesktopName:\s*true/,
		);
	});

	it("应将登录页协议 PDF 复制到生产环境 resources 根目录", () => {
		const config = readFileSync(builderConfigPath, "utf8");

		// 中文注释：主进程生产环境通过 process.resourcesPath 直接打开 PDF，打包时必须复制到该目录。
		expect(config).toMatch(
			/extraResources:[\s\S]*from:\s*resources\/terms-of-service\.pdf[\s\S]*to:\s*terms-of-service\.pdf/,
		);
		expect(config).toMatch(
			/extraResources:[\s\S]*from:\s*resources\/privacy-policy\.pdf[\s\S]*to:\s*privacy-policy\.pdf/,
		);
	});
});
