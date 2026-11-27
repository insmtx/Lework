// 文件下载适配器：与更新包下载一样，原生模块只在 async 函数里动态 import，
// 这样纯路径/URL 拼接可以在 node 环境单测。
import type { Settings } from "../settings/settings";

const CACHE_PREFIX = "lework-preview-";

export interface DownloadFileOptions {
	url: string;
	headers: Record<string, string>;
	destination: string;
	onProgress?: (percent: number) => void;
}

// fileDownloadUrl 拼出控制面的流式下载地址；路径和目录都必须整体编码，
// 否则含空格或 & 的文件名会把 query 拆坏。
export function fileDownloadUrl(
	settings: Settings,
	nodeID: string,
	directory: string,
	path: string,
): string {
	const base = settings.baseUrl.replace(/\/+$/, "");
	return `${base}/api/nodes/${encodeURIComponent(nodeID)}/files/download?directory=${encodeURIComponent(directory)}&path=${encodeURIComponent(path)}`;
}

// fingerprint 是 FNV-1a 32 位：同一缓存目录里不同目录的同名文件（a/report.pdf
// 与 b/report.pdf）不会互相覆盖。
function fingerprint(value: string): string {
	let hash = 0x811c9dc5;
	for (let index = 0; index < value.length; index += 1) {
		hash ^= value.charCodeAt(index);
		hash = Math.imul(hash, 0x01000193);
	}
	return (hash >>> 0).toString(16).padStart(8, "0");
}

function baseName(path: string): string {
	const split = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
	return path.slice(split + 1) || "file";
}

export function previewCachePath(cacheDirectory: string, nodeID: string, path: string): string {
	const base = cacheDirectory.endsWith("/") ? cacheDirectory : `${cacheDirectory}/`;
	return `${base}${CACHE_PREFIX}${fingerprint(`${nodeID}\u0000${path}`)}-${baseName(path)}`;
}

// downloadFile 成功时 resolve 缓存里的 file:// URI；先删掉上次中断留下的
// 同名半成品，避免把截断的文件交给系统查看器。
export async function downloadFile({
	url,
	headers,
	destination,
	onProgress,
}: DownloadFileOptions): Promise<string> {
	const { createDownloadResumable, deleteAsync } = await import("expo-file-system/legacy");

	try {
		await deleteAsync(destination, { idempotent: true });
	} catch {
		// 清理失败不阻塞下载：覆盖写会重新落盘。
	}

	const resumable = createDownloadResumable(
		url,
		destination,
		{ headers },
		onProgress
			? (data) => {
					// 服务端没给 Content-Length 时总大小为 -1，无法计算进度。
					if (data.totalBytesExpectedToWrite > 0) {
						const percent = Math.round(
							(data.totalBytesWritten / data.totalBytesExpectedToWrite) * 100,
						);
						onProgress(Math.min(100, Math.max(0, percent)));
					}
				}
			: undefined,
	);

	const result = await resumable.downloadAsync();
	if (!result?.uri || result.status < 200 || result.status >= 300) {
		throw new Error(`下载文件失败：HTTP ${result?.status ?? "未知"}`);
	}
	return result.uri;
}
