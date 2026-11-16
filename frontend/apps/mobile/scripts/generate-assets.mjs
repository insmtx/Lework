/**
 * 生成移动端品牌资源。
 *
 * 唯一来源是 packages/app-ui/assets/logo.svg 里内嵌的 PNG；不要手工替换生成物，
 * 改 logo 后重新运行：
 *   pnpm --filter @lework/mobile run assets
 */
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import sharp from "sharp";

const scriptDir = fileURLToPath(new URL(".", import.meta.url));
const assetsDir = new URL("../assets/", import.meta.url);
const logoSvg = new URL("../../../packages/app-ui/assets/logo.svg", import.meta.url);

const svg = await readFile(logoSvg, "utf8");
const match = svg.match(/data:image\/png;base64,([A-Za-z0-9+/=]+)/);
if (!match) throw new Error("logo.svg 中未找到内嵌 PNG");

const source = Buffer.from(match[1], "base64");
const trimmed = await sharp(source).trim().toBuffer({ resolveWithObject: true });

async function paddedSquare(size, ratio, background) {
	const inner = Math.round(size * ratio);
	const resized = await sharp(trimmed.data)
		.resize(inner, inner, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 0 } })
		.png()
		.toBuffer();
	return sharp({ create: { width: size, height: size, channels: 4, background } })
		.composite([{ input: resized, gravity: "center" }])
		.png()
		.toBuffer();
}

async function transparentSquare(size, ratio) {
	return paddedSquare(size, ratio, { r: 0, g: 0, b: 0, alpha: 0 });
}

async function notificationIcon(size) {
	const inner = Math.round(size * 0.9);
	const alpha = await sharp(trimmed.data)
		.resize(inner, inner, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 0 } })
		.ensureAlpha()
		.extractChannel(3)
		.png()
		.toBuffer();
	const white = await sharp({
		create: { width: size, height: size, channels: 3, background: { r: 255, g: 255, b: 255 } },
	})
		.png()
		.toBuffer();
	const paddedAlpha = await sharp(alpha)
		.resize(size, size, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 1 } })
		.toColourspace("b-w")
		.png()
		.toBuffer();
	return sharp(white).joinChannel(paddedAlpha).png().toBuffer();
}

const files = {
	"icon.png": await paddedSquare(1024, 0.82, { r: 255, g: 255, b: 255, alpha: 1 }),
	"adaptive-icon.png": await transparentSquare(1024, 0.64),
	"splash-icon.png": await transparentSquare(512, 0.78),
	"logo.png": await transparentSquare(256, 0.96),
	"notification-icon.png": await notificationIcon(96),
};

for (const [name, buffer] of Object.entries(files)) {
	await writeFile(new URL(name, assetsDir), buffer);
	console.log(`generated ${name} (${buffer.length} bytes) from ${scriptDir}`);
}
