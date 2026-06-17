import { mkdir } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const currentDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(currentDir, "..");
const sourceLogo = resolve(appDir, "../../packages/app-ui/assets/logo.svg");
const resourcesDir = join(appDir, "resources");

await mkdir(resourcesDir, { recursive: true });
await sharp(await renderIcon(1024)).toFile(join(resourcesDir, "icon.png"));

async function renderIcon(size) {
	const cornerRadius = Math.round(size * 0.2);
	const logoScale = size <= 64 ? 0.82 : 0.72;
	const logoSize = Math.round(size * logoScale);
	const logoOffset = Math.round((size - logoSize) / 2);

	const background = Buffer.from(`
		<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" xmlns="http://www.w3.org/2000/svg">
			<defs>
				<linearGradient id="bg" x1="0" y1="0" x2="0" y2="1">
					<stop offset="0" stop-color="#F8FAFC"/>
					<stop offset="1" stop-color="#E7EBF0"/>
				</linearGradient>
			</defs>
			<rect x="1" y="1" width="${size - 2}" height="${size - 2}" rx="${cornerRadius}" fill="url(#bg)"/>
			<rect x="1.5" y="1.5" width="${size - 3}" height="${size - 3}" rx="${cornerRadius}" fill="none" stroke="#D7DDE5" stroke-width="${Math.max(1, size / 128)}"/>
		</svg>
	`);

	const logo = await sharp(sourceLogo).resize(logoSize, logoSize, { fit: "contain" }).png().toBuffer();

	return sharp({
		create: {
			width: size,
			height: size,
			channels: 4,
			background: { r: 0, g: 0, b: 0, alpha: 0 },
		},
	})
		.composite([
			{ input: background },
			{ input: logo, left: logoOffset, top: logoOffset },
		])
		.png()
		.toBuffer();
}
