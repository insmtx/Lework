// 桌面端更新源固定指向 COS 统一发布目录（application/<version>/<platform>/<arch>/），
// 只允许环境变量 LEWORK_UPDATE_BASE_URL 覆盖（本地联调/测试用）。禁止从运行时设置
// 推导，防止控制面地址变化把更新流量带偏。与移动端 update/source.ts 保持同构（R4）。

// 用 || 而非 ??：空字符串覆盖没有意义，必须回落到 COS 默认地址。
export const DESKTOP_UPDATE_BASE_URL: string =
	process.env.LEWORK_UPDATE_BASE_URL ||
	"https://leros-1395325824.cos.ap-beijing.myqcloud.com/application";

// 与 linux-update.ts 的 parseVersion 语法一致；LATEST.txt 里只应是纯版本号，
// HTML 错误页会落空。可选前导 v 由 isVersionNewer 比较时容忍。
const LATEST_VERSION_PATTERN = /^v?\d+(?:\.\d+)*(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

/** parseLatestVersion 取 LATEST.txt 首个非空行并校验为版本号，非法内容抛错。 */
export function parseLatestVersion(body: string): string {
	const line = body
		.split(/\r?\n/)
		.map((value) => value.trim())
		.find((value) => value.length > 0);
	if (!line) {
		throw new Error("LATEST.txt 内容为空");
	}
	if (!LATEST_VERSION_PATTERN.test(line)) {
		throw new Error(`LATEST.txt 内容非法：${line}`);
	}

	return line;
}

export type DesktopTarget = {
	dir: string;
	metadataFile: "latest-mac.yml" | "latest.yml" | "latest-linux.yml";
};

// 只收录 GitHub 发布流水线实际构建上传的四种组合；其余平台不提供桌面更新。
const DESKTOP_TARGETS = new Map<string, DesktopTarget>([
	["darwin/arm64", { dir: "darwin/arm64", metadataFile: "latest-mac.yml" }],
	["darwin/x64", { dir: "darwin/x64", metadataFile: "latest-mac.yml" }],
	["win32/x64", { dir: "win32/x64", metadataFile: "latest.yml" }],
	["linux/x64", { dir: "linux/x64", metadataFile: "latest-linux.yml" }],
]);

/** desktopTarget 映射 platform/arch 到版本目录与元数据文件名，不支持的组合返回 null。 */
export function desktopTarget(platform: NodeJS.Platform, arch: string): DesktopTarget | null {
	const target = DESKTOP_TARGETS.get(`${platform}/${arch}`);
	if (!target) {
		return null;
	}

	return { ...target };
}

function trimTrailingSlash(url: string): string {
	return url.replace(/\/+$/, "");
}

/** feedUrl 指向版本目录下的平台产物前缀，供 electron-updater generic provider 使用。 */
export function feedUrl(baseUrl: string, version: string, target: DesktopTarget): string {
	const base = trimTrailingSlash(baseUrl);
	const release = trimTrailingSlash(version.trim());
	return `${base}/${release}/${target.dir}`;
}

/** metadataUrl 带时间戳查询参数规避 COS 缓存；now 缺省取 Date.now()。 */
export function metadataUrl(
	baseUrl: string,
	version: string,
	target: DesktopTarget,
	now: number = Date.now(),
): string {
	return `${feedUrl(baseUrl, version, target)}/${target.metadataFile}?t=${now}`;
}

/** latestTxtUrl 读取最新稳定版本号；now 缺省取 Date.now()。 */
export function latestTxtUrl(
	baseUrl: string = DESKTOP_UPDATE_BASE_URL,
	now: number = Date.now(),
): string {
	const base = trimTrailingSlash(baseUrl);
	return `${base}/LATEST.txt?t=${now}`;
}
