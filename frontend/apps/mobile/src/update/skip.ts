// 用户跳过的版本号：本机偏好，不进控制面。App 里没有 AsyncStorage/MMKV，
// 沿用已有的 SecureStore（与已读水位、草稿一致）。
import * as SecureStore from "expo-secure-store";

/** SKIPPED_VERSION_KEY 是 SecureStore 的键，只允许字母数字与 ._-。 */
export const SKIPPED_VERSION_KEY = "lework.update.skipped.v1";

/** readSkippedVersion 读失败按「没跳过」处理，不让存储问题挡住更新检查。 */
export async function readSkippedVersion(): Promise<string | null> {
	try {
		return await SecureStore.getItemAsync(SKIPPED_VERSION_KEY);
	} catch {
		return null;
	}
}

/** writeSkippedVersion 写失败被忽略：最坏结果是下次检查再提示一次。 */
export async function writeSkippedVersion(version: string): Promise<void> {
	try {
		await SecureStore.setItemAsync(SKIPPED_VERSION_KEY, version);
	} catch {
		// 忽略：见函数注释。
	}
}
