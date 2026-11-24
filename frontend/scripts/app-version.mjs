import { execFileSync } from "node:child_process";

/**
 * resolveAppVersion 是发布版本号的唯一来源。
 *
 * CI 用发布 tag 注入 APP_VERSION；本地构建没有 tag 时退回 git describe，
 * 再退到 dev 后缀，保证三个应用永远拿到同一串版本号。
 */
export function resolveAppVersion({ env = {}, describe, sha } = {}) {
	const explicit = env.APP_VERSION?.trim();
	if (explicit) return explicit;

	const tag = (describe ?? defaultDescribe)()?.trim();
	if (tag) return tag.replace(/^v/, "");

	const shortSha = sha?.trim().slice(0, 7);
	return shortSha ? `0.0.0-dev.${shortSha}` : "0.0.0";
}

function defaultDescribe() {
	try {
		return execFileSync("git", ["describe", "--tags", "--abbrev=0"], {
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		});
	} catch {
		return null;
	}
}
