import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const builderConfigPath = resolve(__dirname, "../../../electron-builder.yml");

describe("electron-builder 桌面图标配置", () => {
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
});
