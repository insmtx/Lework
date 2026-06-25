import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const builderConfigPath = resolve(
	__dirname,
	"../../../electron-builder.yml",
);

describe("electron-builder Windows 图标配置", () => {
	it("不应禁用 Windows 可执行文件资源编辑，否则安装后的图标会回退为默认值", () => {
		const config = readFileSync(builderConfigPath, "utf8");

		// 中文注释：Windows 的 EXE 图标资源写入依赖 signAndEditExecutable，显式关闭会导致快捷方式和任务栏图标丢失。
		expect(config).not.toMatch(/signAndEditExecutable:\s*false/);
	});
});
