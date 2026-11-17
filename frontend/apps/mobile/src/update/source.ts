// 更新源固定指向 COS 发布目录，只允许构建期 EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL
// 覆盖（本地联调用）。禁止从 settings.baseUrl 或任何运行时设置推导，防止控制面
// 地址变化把更新流量带偏。

// 用 || 而非 ??：空字符串覆盖没有意义，必须回落到 COS 默认地址。
export const MOBILE_UPDATE_BASE_URL: string =
	process.env.EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL ||
	"https://leros-1395325824.cos.ap-beijing.myqcloud.com/application";

/** Android 应用包名，与 app.json 的 android.package 一致，供安装 intent 使用。 */
export const MOBILE_APP_ID = "com.insmtx.lework";

// 与 version.ts 的版本语法一致；LATEST.txt 沿用旧下载页的指针文件，只应是纯版本号，
// HTML 错误页会落空。
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

function trimTrailingSlash(url: string): string {
	return url.replace(/\/+$/, "");
}

/** latestTxtUrl 带时间戳查询参数规避 COS 缓存；now 缺省取 Date.now()。 */
export function latestTxtUrl(
	baseUrl: string = MOBILE_UPDATE_BASE_URL,
	now: number = Date.now(),
): string {
	const base = trimTrailingSlash(baseUrl);
	return `${base}/LATEST.txt?t=${now}`;
}

/** packageUrl 按发布约定拼接版本目录下的 Android 更新包地址。
 *
 * COS 禁止匿名下载 `.apk`，发布产物统一压成同名 zip，App 下载后在本地解压出
 * APK 再交给系统安装器。
 */
export function packageUrl(version: string, baseUrl: string = MOBILE_UPDATE_BASE_URL): string {
	const base = trimTrailingSlash(baseUrl);
	return `${base}/${version}/android/lework-android-${version}.zip`;
}
