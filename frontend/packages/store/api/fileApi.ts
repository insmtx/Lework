import { authenticatedFetch } from "../utils/authStorage";
import { API_BASE_URL } from "./config";

export function getFileDownloadUrl(publicId: string): string {
	return `${API_BASE_URL}/files/${encodeURIComponent(publicId)}/download`;
}

export async function fetchFileDownload(
	publicId: string,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	const response = await authenticatedFetch(getFileDownloadUrl(publicId), {
		method: "GET",
		signal: options?.signal,
	});
	if (!response.ok) {
		throw new Error(`HTTP ${response.status}`);
	}
	return response;
}

export function getFilePreviewUrl(storageUri: string): string {
	return `${API_BASE_URL}/files/preview?storage_uri=${encodeURIComponent(storageUri)}`;
}

export function getFilePreviewUrlByPublicId(publicId: string): string {
	return `${API_BASE_URL}/files/preview?public_id=${encodeURIComponent(publicId)}`;
}

// 中文注释：通过 storage_uri 预览/下载文件，需携带 JWT 认证
export async function fetchFilePreviewByStorageUri(
	storageUri: string,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	const response = await authenticatedFetch(getFilePreviewUrl(storageUri), {
		method: "GET",
		signal: options?.signal,
	});
	if (!response.ok) {
		throw new Error(`HTTP ${response.status}`);
	}
	return response;
}

// 中文注释：通过 public_id 预览/下载文件，需携带 JWT 认证
export async function fetchFilePreviewByPublicId(
	publicId: string,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	const response = await authenticatedFetch(getFilePreviewUrlByPublicId(publicId), {
		method: "GET",
		signal: options?.signal,
	});
	if (!response.ok) {
		throw new Error(`HTTP ${response.status}`);
	}
	return response;
}

// 中文注释：统一走 preview 接口，优先 storage_uri，其次 public_id
export async function fetchFilePreview(
	identity: { storageUri?: string; publicId?: string },
	options?: { signal?: AbortSignal },
): Promise<Response> {
	const storageUri = identity.storageUri?.trim();
	if (storageUri) {
		return fetchFilePreviewByStorageUri(storageUri, options);
	}
	const publicId = identity.publicId?.trim();
	if (publicId) {
		return fetchFilePreviewByPublicId(publicId, options);
	}
	throw new Error("文件缺少 preview 标识");
}

export const fileApi = {
	getDownloadUrl: getFileDownloadUrl,
	fetchDownload: fetchFileDownload,
	getPreviewUrl: getFilePreviewUrl,
	getPreviewUrlByPublicId: getFilePreviewUrlByPublicId,
	fetchPreviewByStorageUri: fetchFilePreviewByStorageUri,
	fetchPreviewByPublicId: fetchFilePreviewByPublicId,
	fetchPreview: fetchFilePreview,
};
