import { spawn } from "node:child_process";
import process from "node:process";

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

await run("pnpm", ["run", "icons"]);
await run("pnpm", ["run", "compile"]);
await run("electron-builder", [...targets, "--publish", "never"]);

function run(command, args) {
	return new Promise((resolve, reject) => {
		const child = spawn(command, args, {
			stdio: "inherit",
			shell: process.platform === "win32",
		});

		child.on("error", reject);
		child.on("close", (code) => {
			if (code === 0) {
				resolve();
				return;
			}

			reject(new Error(`${command} ${args.join(" ")} exited with code ${code}`));
		});
	});
}
