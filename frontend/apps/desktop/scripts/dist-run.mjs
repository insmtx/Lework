import { spawn } from "node:child_process";
import process from "node:process";

export function run(command, args) {
	return new Promise((resolve, reject) => {
		const child = spawn(command, args, {
			stdio: "inherit",
			shell: process.platform === "win32",
			env: process.env,
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

function privateArtifactNameArgs() {
	if (process.env.VITE_LEROS_DEPLOYMENT_MODE !== "private") {
		return [];
	}

	// 中文注释：私有化安装包文件名加 -private，避免和 SaaS 包同名无法区分。
	return [
		"-c.mac.artifactName=${productName}-${version}-mac-${arch}-private.${ext}",
		"-c.win.artifactName=${productName}-${version}-win-${arch}-private.${ext}",
		"-c.linux.artifactName=${productName}-${version}-linux-${arch}-private.${ext}",
	];
}

export async function runDesktopDist(builderArgs) {
	await run("pnpm", ["run", "icons"]);
	await run("pnpm", ["run", "compile"]);
	await run("electron-builder", [
		...builderArgs,
		...privateArtifactNameArgs(),
		"--publish",
		"never",
	]);
}
