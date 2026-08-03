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

export async function runDesktopDist(builderArgs) {
	await run("pnpm", ["run", "icons"]);
	await run("pnpm", ["run", "compile"]);
	await run("electron-builder", [...builderArgs, "--publish", "never"]);
}
