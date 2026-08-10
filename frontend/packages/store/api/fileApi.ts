import { authenticatedFetch } from "../utils/authStorage";
import { API_BASE_URL } from "./config";

function getAPIOriginURL(): string {
	return API_BASE_URL.replace(/\/v1$/, "");
}

// normalizeFilePublicId 统一文件标识，并兼容历史上误保存的旧 download URL。
export function normalizeFilePublicId(value?: string | null): string | undefined {
	const trimmed = value?.trim();
	if (!trimmed) return undefined;
	if (/^file_[A-Za-z0-9_-]+$/.test(trimmed)) return trimmed;

	const legacyMatch = trimmed.match(/\/files\/(file_[A-Za-z0-9_-]+)\/download(?:[/?#]|$)/);
	return legacyMatch?.[1];
}

export function getFilePublicUrlFromStorageUri(storageUri?: string): string | undefined {
	const uri = storageUri?.trim();
	if (!uri?.startsWith("file://")) return undefined;

	const parts = uri.slice("file://".length).replace(/^\/+/, "").split("/").filter(Boolean);
	if (parts.length < 2) return undefined;

	return `${getAPIOriginURL()}/${parts.map(encodeURIComponent).join("/")}`;
}

// 中文注释：预览内容统一走服务端流式下载，避免 /files/preview 302 到
// storage.base_url（私有化常为内网主机如 leros:8080）导致浏览器 ERR_NAME_NOT_RESOLVED。
export function getFilePreviewUrl(storageUri: string): string {
	return `${API_BASE_URL}/files/download?storage_uri=${encodeURIComponent(storageUri)}`;
}

export function getFilePreviewUrlByPublicId(publicId: string): string {
	return `${API_BASE_URL}/files/download?public_id=${encodeURIComponent(publicId)}`;
}

// 中文注释：通过 storage_uri 预览/下载文件，需携带 JWT 认证；内容由服务端代理流式返回
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

// 中文注释：通过 public_id 预览/下载文件，需携带 JWT 认证；内容由服务端代理流式返回
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

// 中文注释：统一走服务端流式 download，优先 storage_uri，其次 public_id
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
	getPublicUrlFromStorageUri: getFilePublicUrlFromStorageUri,
	getPreviewUrl: getFilePreviewUrl,
	getPreviewUrlByPublicId: getFilePreviewUrlByPublicId,
	fetchPreviewByStorageUri: fetchFilePreviewByStorageUri,
	fetchPreviewByPublicId: fetchFilePreviewByPublicId,
	fetchPreview: fetchFilePreview,
};
