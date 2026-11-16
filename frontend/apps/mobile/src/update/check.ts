import { apkUrl, latestTxtUrl, parseLatestVersion } from "./source";
import { isVersionNewer } from "./version";

export type CheckResult =
	| { kind: "update"; version: string; apkUrl: string }
	| { kind: "none" }
	| { kind: "error"; message: string };

export type CheckForUpdateOptions = {
	currentVersion: string;
	fetchImpl?: typeof fetch;
	baseUrl?: string;
	now?: () => number;
};

// checkForUpdate 只认构建期更新源（source.ts），绝不读取 settings.baseUrl。
// 流程：GET LATEST.txt（带时间戳防缓存）→ 解析并比较 → 有新版本再 HEAD 确认
// APK 已发布；产物缺失返回 none，任何解析/HTTP/网络失败返回 error，由上层决定
// 自动检查静默、手动检查提示。
export async function checkForUpdate(options: CheckForUpdateOptions): Promise<CheckResult> {
	const fetchImpl = options.fetchImpl ?? fetch;

	try {
		const latestResponse = await fetchImpl(latestTxtUrl(options.baseUrl, options.now?.()), {
			cache: "no-store",
		});
		if (!latestResponse.ok) {
			return {
				kind: "error",
				message: `获取 LATEST.txt 失败：HTTP ${latestResponse.status}`,
			};
		}

		const version = parseLatestVersion(await latestResponse.text());
		if (!isVersionNewer(version, options.currentVersion)) {
			return { kind: "none" };
		}

		const url = apkUrl(version, options.baseUrl);
		const apkResponse = await fetchImpl(url, { method: "HEAD", cache: "no-store" });
		// 版本号已写入但产物尚未上传完成时按"无更新"处理，等待下次检查。
		if (!apkResponse.ok) {
			return { kind: "none" };
		}

		return { kind: "update", version, apkUrl: url };
	} catch (error) {
		return {
			kind: "error",
			message: error instanceof Error ? error.message : String(error),
		};
	}
}
