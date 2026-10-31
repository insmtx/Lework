"use client";

import {
	authenticatedFetch,
	fetchFilePreviewByPublicId,
	normalizeFilePublicId,
} from "@lework/store";
import { type ReactNode, useEffect, useState } from "react";

const PROTECTED_IMAGE_CACHE_PREFIX = "lework-avatar-cache:";

const memoryCache = new Map<string, string>();
const inflightLoads = new Map<string, Promise<string>>();

function isFilePublicId(src: string): boolean {
	return /^file_[A-Za-z0-9_-]+$/.test(src.trim());
}

function isProtectedImageSource(src: string): boolean {
	return isFilePublicId(src) || Boolean(normalizeFilePublicId(src));
}

function getProtectedImageCacheKey(src: string): string {
	return `${PROTECTED_IMAGE_CACHE_PREFIX}${src}`;
}

function getProtectedImageLoadKey(src: string): string {
	return src.trim();
}

function readProtectedImageDataURLSync(src?: string | null): string | null {
	if (!src || !isProtectedImageSource(src)) return null;
	const key = getProtectedImageLoadKey(src);
	const fromMemory = memoryCache.get(key);
	if (fromMemory) return fromMemory;
	return getCachedProtectedImageDataURL(src);
}

function loadProtectedImageDataURL(src: string): Promise<string> {
	const key = getProtectedImageLoadKey(src);

	const fromMemory = memoryCache.get(key);
	if (fromMemory) return Promise.resolve(fromMemory);

	const fromStorage = getCachedProtectedImageDataURL(src);
	if (fromStorage) {
		memoryCache.set(key, fromStorage);
		return Promise.resolve(fromStorage);
	}

	const inflight = inflightLoads.get(key);
	if (inflight) return inflight;

	const promise = fetchProtectedImageSource(src)
		.then(async (response) => {
			if (!response.ok) throw new Error(`HTTP ${response.status}`);
			const blob = await response.blob();
			return blobToDataURL(blob);
		})
		.then((dataURL) => {
			memoryCache.set(key, dataURL);
			cacheProtectedImageDataURL(src, dataURL);
			return dataURL;
		})
		.finally(() => {
			inflightLoads.delete(key);
		});

	inflightLoads.set(key, promise);
	return promise;
}

/** 供原生 DOM 场景读取头像；受保护文件会先转换为可展示的 data URL。 */
export function loadProtectedImageDisplayURL(src: string): Promise<string> {
	if (!isProtectedImageSource(src)) return Promise.resolve(src);
	return loadProtectedImageDataURL(src);
}

/** @internal test helper */
export function resetProtectedImageCacheForTests() {
	memoryCache.clear();
	inflightLoads.clear();
}

export function getCachedProtectedImageDataURL(src?: string | null): string | null {
	if (!src || typeof window === "undefined" || !isProtectedImageSource(src)) return null;
	try {
		return window.localStorage.getItem(getProtectedImageCacheKey(src));
	} catch {
		return null;
	}
}

export function cacheProtectedImageDataURL(src: string, dataURL: string) {
	if (typeof window === "undefined" || !isProtectedImageSource(src)) return;
	memoryCache.set(getProtectedImageLoadKey(src), dataURL);
	try {
		window.localStorage.setItem(getProtectedImageCacheKey(src), dataURL);
	} catch {
		// Optional UX optimization.
	}
}

export function blobToDataURL(blob: Blob): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.addEventListener("load", () => {
			if (typeof reader.result === "string") {
				resolve(reader.result);
				return;
			}
			reject(new Error("图片读取失败"));
		});
		reader.addEventListener("error", () => reject(new Error("图片读取失败")));
		reader.readAsDataURL(blob);
	});
}

function fetchProtectedImageSource(src: string): Promise<Response> {
	const publicId = normalizeFilePublicId(src);
	if (publicId) {
		// 中文注释：头像字段统一保存 public_id，历史 download URL 也会先转换后走 preview。
		return fetchFilePreviewByPublicId(publicId);
	}

	return authenticatedFetch(src);
}

type ProtectedImageProps = {
	src?: string | null;
	localSrc?: string | null;
	alt: string;
	className: string;
	fallback: ReactNode;
	loadingFallback?: ReactNode;
	onProtectedSrcNotFound?: () => void;
	onProtectedSrcLoaded?: () => void;
};

export function ProtectedImage({
	src,
	localSrc,
	alt,
	className,
	fallback,
	loadingFallback,
	onProtectedSrcNotFound,
	onProtectedSrcLoaded,
}: ProtectedImageProps) {
	const [failed, setFailed] = useState(false);
	const [imageURL, setImageURL] = useState<string | null>(() => readProtectedImageDataURLSync(src));

	useEffect(() => {
		setFailed(false);
		if (!src || !isProtectedImageSource(src)) {
			setImageURL(null);
			return;
		}

		const cachedImageURL = readProtectedImageDataURLSync(src);
		if (cachedImageURL) {
			setImageURL(cachedImageURL);
			onProtectedSrcLoaded?.();
			return;
		}

		let cancelled = false;
		loadProtectedImageDataURL(src)
			.then((dataURL) => {
				if (cancelled) return;
				setImageURL(dataURL);
				onProtectedSrcLoaded?.();
			})
			.catch((error) => {
				if (cancelled) return;
				const isNotFoundError =
					error instanceof Error && (error.message === "HTTP 404" || error.message.includes("404"));
				if (isNotFoundError) {
					onProtectedSrcNotFound?.();
				}
				setFailed(true);
			});

		return () => {
			cancelled = true;
		};
	}, [onProtectedSrcLoaded, onProtectedSrcNotFound, src]);

	if (localSrc) {
		return (
			<img
				src={localSrc}
				alt={alt}
				className={className}
				loading="lazy"
				decoding="async"
				referrerPolicy="no-referrer"
			/>
		);
	}

	if (!src || failed) return <>{fallback}</>;
	const imageSrc = imageURL || src;
	if (isProtectedImageSource(src) && !imageURL) return <>{loadingFallback ?? fallback}</>;

	return (
		<img
			src={imageSrc}
			alt={alt}
			className={className}
			loading="lazy"
			decoding="async"
			referrerPolicy="no-referrer"
			onError={() => setFailed(true)}
		/>
	);
}
