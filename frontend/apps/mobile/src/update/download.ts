// 更新包（zip）下载适配器：包装 expo-file-system/legacy 的 DownloadResumable，对上层只暴露
// 两个纯函数与一个 Promise<string>。原生模块按 ruling R3 处理：类型用 import type
// 引入（编译期擦除，不影响 node 环境 vitest），运行时在 async 函数内动态 import；
// 纯函数 updatePackagePath / clampProgress 不触碰原生模块，可单独导入测试。
import type {
	DownloadOptions,
	DownloadProgressData,
	DownloadResumable,
	FileSystemNetworkTaskProgressCallback,
} from "expo-file-system/legacy";

type DownloadPackageOptions = {
	url: string;
	destination: string;
	onProgress?: (percent: number) => void;
	// 可选取消通道（controller ruling）：abort 时 cancelAsync 真实任务、清理半成品
	// 并 reject「更新包下载已取消」。不传时行为与旧调用方完全一致。
	signal?: AbortSignal;
};

// 进度按整数百分比上报：四舍五入后夹在 0–100，与状态机（state.ts）的取值口径一致，
// 异常进度值不会穿透到界面。
export function clampProgress(percent: number): number {
	return Math.min(100, Math.max(0, Math.round(percent)));
}

// 目标路径约定为 cacheDirectory + "lework-update-<version>.zip"；结尾多余的分隔符
// 由这里统一，避免调用方拼接习惯不同产生双斜杠路径。
export function updatePackagePath(cacheDirectory: string, version: string): string {
	const base = cacheDirectory.endsWith("/") ? cacheDirectory : `${cacheDirectory}/`;
	return `${base}lework-update-${version}.zip`;
}

// downloadPackage 成功时 resolve 已下载文件的 file:// URI；结果缺失或状态非 2xx 抛出
// 可读的中文错误。任何失败路径都会先清理半成品文件再抛错（清理失败不掩盖原始错误），
// 由调用方 dispatch failed / 决定重试。传入 signal 且被 abort 时抛「更新包下载已取消」，
// 调用方据此静默收尾，不再向状态机 dispatch failed。
export async function downloadPackage({
	url,
	destination,
	onProgress,
	signal,
}: DownloadPackageOptions): Promise<string> {
	const { createDownloadResumable, deleteAsync } = await import("expo-file-system/legacy");

	const deleteQuietly = async (): Promise<void> => {
		try {
			await deleteAsync(destination, { idempotent: true });
		} catch {
			// 清理半成品失败时忽略，避免掩盖原始错误。
		}
	};

	const isAborted = (): boolean => signal?.aborted === true;

	// 开始前删除同目录旧文件（设计文档要求）：上次下载中断留下的半成品不可续传，
	// 覆盖安装会因文件损坏失败，必须整份重新下载。
	await deleteQuietly();

	if (isAborted()) {
		// 排队期间被取消（如 dismiss 先落）：任务还没创建，直接按取消收场。
		throw new Error("更新包下载已取消");
	}

	const options: DownloadOptions = {};
	const progressCallback: FileSystemNetworkTaskProgressCallback<DownloadProgressData> | undefined =
		onProgress
			? (data) => {
					// 服务端未返回 Content-Length 时总大小为 -1，此时无法计算进度，跳过上报。
					if (data.totalBytesExpectedToWrite > 0) {
						onProgress(
							clampProgress((data.totalBytesWritten / data.totalBytesExpectedToWrite) * 100),
						);
					}
				}
			: undefined;

	const resumable: DownloadResumable = createDownloadResumable(
		url,
		destination,
		options,
		progressCallback,
	);

	// abort 只负责踢一脚：cancelAsync 让 downloadAsync 尽快 resolve(undefined)
	// 或抛错，真正的清理与抛错统一走下面的收尾路径。cancelAsync 本身失败可忽略。
	const onAbort = (): void => {
		void resumable.cancelAsync().catch(() => {
			// cancelAsync 失败忽略：downloadAsync 已在收尾，统一清理并按取消抛错。
		});
	};
	signal?.addEventListener("abort", onAbort, { once: true });

	try {
		const result = await resumable.downloadAsync();
		// 取消与下载完成同帧竞态时以取消为准：上层已回到 idle，不能让它
		// 突然收到 download-finished。
		if (isAborted()) {
			throw new Error("更新包下载已取消");
		}
		if (!result?.uri || result.status < 200 || result.status >= 300) {
			throw new Error(`下载更新包失败：HTTP ${result?.status ?? "未知"}`);
		}
		return result.uri;
	} catch (error) {
		await deleteQuietly();
		if (isAborted()) {
			throw new Error("更新包下载已取消");
		}
		if (error instanceof Error && error.message.startsWith("下载更新包失败")) {
			throw error; // 已经是可读错误，避免二次包装。
		}
		throw new Error(`下载更新包失败：${error instanceof Error ? error.message : String(error)}`);
	} finally {
		signal?.removeEventListener("abort", onAbort);
	}
}
