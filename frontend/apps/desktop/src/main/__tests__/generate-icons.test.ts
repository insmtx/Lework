import { access } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";
import { describe, expect, it } from "vitest";
import { renderIcon, renderMacIcon } from "../../../scripts/generate-icons.mjs";

const currentDir = dirname(fileURLToPath(import.meta.url));
const resourcesDir = resolve(currentDir, "../../../resources");

describe("generate-icons 平台图标生成", () => {
	it("macOS 图标应使用不透明底板并保留安全边距", async () => {
		const iconBuffer = await renderMacIcon(512);
		const metadata = await sharp(iconBuffer).metadata();
		const stats = await sharp(iconBuffer).stats();

		expect(metadata.width).toBe(512);
		expect(metadata.height).toBe(512);
		expect(metadata.hasAlpha).not.toBe(true);
		expect(stats.isOpaque).toBe(true);
	});

	it("Windows 图标应保持透明背景", async () => {
		const iconBuffer = await renderIcon(256);
		const metadata = await sharp(iconBuffer).metadata();

		expect(metadata.hasAlpha).toBe(true);
	});

	it("应生成 macOS 打包所需的 icon-mac.png 资源", async () => {
		await expect(access(join(resourcesDir, "icon-mac.png"))).resolves.toBeUndefined();
	});
});
