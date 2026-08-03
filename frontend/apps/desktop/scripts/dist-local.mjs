import process from "node:process";
import { runDesktopDist } from "./dist-run.mjs";

const platformTargets = {
	darwin: ["--mac", "zip"],
	win32: ["--win", "nsis"],
	linux: ["--linux", "AppImage", "deb"],
};

const targets = platformTargets[process.platform];

if (!targets) {
	console.error(`Unsupported desktop packaging platform: ${process.platform}`);
	process.exit(1);
}

await runDesktopDist(targets);
