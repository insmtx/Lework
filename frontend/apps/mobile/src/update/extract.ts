// 更新包解压适配器：COS 禁止匿名下载 apk，发布产物改成同名 zip，这里负责把 zip
// 里唯一的 APK 条目写回缓存目录，交给安装器。原生模块按 ruling R3 处理：类型/运行
// 时都放在函数体内动态 import，纯函数 pickApkEntry 可脱离原生模块单测。
import { unzipSync } from "fflate";

export type ExtractApkOptions = {
	/** 已下载的 zip 包 file:// URI。 */
	zipUri: string;
	/** 解压出的 apk 目标 file:// URI，已存在时会被覆盖。 */
	apkUri: string;
};

/**
 * extractedApkPath 给出解压后的 apk 缓存路径，与下载的 zip 同目录同版本号。
 */
export function extractedApkPath(cacheDirectory: string, version: string): string {
	const base = cacheDirectory.endsWith("/") ? cacheDirectory : `${cacheDirectory}/`;
	return `${base}lework-update-${version}.apk`;
}

/**
 * pickApkEntry 在 zip 条目名里挑唯一的 APK 文件。
 *
 * 目录条目（以 `/` 结尾）不是文件，直接排除；0 个或多个 APK 都返回 null，
 * 由调用方给出可读错误，避免装错包。
 */
export function pickApkEntry(names: readonly string[]): string | null {
	const apkNames = names.filter(
		(name) => !name.endsWith("/") && name.toLowerCase().endsWith(".apk"),
	);
	return apkNames.length === 1 ? apkNames[0] : null;
}

/**
 * extractApkFromZip 解压 zip 并返回 apk 的 file:// URI。
 *
 * 整个包一次性读入内存再解压：APK 本身已压缩，体积与 zip 接近，省掉流式拼接
 * 的复杂度。上次尝试残留的同名 apk 先删除再写，避免安装到损坏文件。
 */
export async function extractApkFromZip({ zipUri, apkUri }: ExtractApkOptions): Promise<string> {
	const { File } = await import("expo-file-system");

	const archive = new File(zipUri);
	const entries = unzipSync(await archive.bytes());
	const entry = pickApkEntry(Object.keys(entries));
	if (!entry) {
		throw new Error("更新包里没有找到唯一的 APK 文件，请重新下载");
	}

	const target = new File(apkUri);
	if (target.exists) target.delete();
	await target.write(entries[entry]);

	// 安装只需要 apk，zip 留着白占几十兆缓存；清理失败不影响本次升级。
	try {
		archive.delete();
	} catch {
		// 忽略清理失败：下次下载前 downloadPackage 也会先删旧包。
	}

	return target.uri;
}
