import { access } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";
import { describe, expect, it } from "vitest";
import { renderIcon, renderMacIcon } from "../../../scripts/generate-icons.mjs";

const currentDir = dirname(fileURLToPath(import.meta.url));
const resourcesDir = resolve(currentDir, "../../../resources");

describe("generate-icons 平台图标生成", () => {
	it("macOS 图标应为圆角底板并在四周保留透明边距", async () => {
		const size = 512;
		const iconBuffer = await renderMacIcon(size);
		const metadata = await sharp(iconBuffer).metadata();

		expect(metadata.width).toBe(size);
		expect(metadata.height).toBe(size);
		// 中文注释：Apple 规范要求图标本体外留透明边距，因此必须保留 alpha 通道。
		expect(metadata.hasAlpha).toBe(true);

		const { data, info } = await sharp(iconBuffer).raw().toBuffer({ resolveWithObject: true });
		const alphaAt = (x: number, y: number) => data[(y * info.width + x) * info.channels + 3];

		// 中文注释：画布四角应完全透明（圆角外区域），中心应被底板完全覆盖。
		expect(alphaAt(2, 2)).toBe(0);
		expect(alphaAt(size - 3, 2)).toBe(0);
		expect(alphaAt(2, size - 3)).toBe(0);
		expect(alphaAt(size - 3, size - 3)).toBe(0);
		expect(alphaAt(Math.floor(size / 2), Math.floor(size / 2))).toBe(255);
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
