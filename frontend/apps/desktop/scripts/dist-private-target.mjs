import process from "node:process";
import { runDesktopDist } from "./dist-run.mjs";

process.env.VITE_LEROS_DEPLOYMENT_MODE = "private";

const builderArgs = process.argv.slice(2);

if (builderArgs.length === 0) {
	console.error("Usage: node scripts/dist-private-target.mjs <electron-builder args...>");
	console.error("Example: node scripts/dist-private-target.mjs --win --x64");
	process.exit(1);
}

await runDesktopDist(builderArgs);
