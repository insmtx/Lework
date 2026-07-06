"use client";

import { authenticatedFetch } from "@leros/store";
import { type ReactNode, useEffect, useState } from "react";

const PROTECTED_IMAGE_CACHE_PREFIX = "leros-avatar-cache:";

export function isProtectedFileURL(src: string): boolean {
	return src.includes("/files/") && src.includes("/download");
}

function getProtectedImageCacheKey(src: string): string {
	return `${PROTECTED_IMAGE_CACHE_PREFIX}${src}`;
}

export function getCachedProtectedImageDataURL(src?: string | null): string | null {
	if (!src || typeof window === "undefined" || !isProtectedFileURL(src)) return null;
	try {
		return window.localStorage.getItem(getProtectedImageCacheKey(src));
	} catch {
		return null;
	}
}

export function cacheProtectedImageDataURL(src: string, dataURL: string) {
	if (typeof window === "undefined" || !isProtectedFileURL(src)) return;
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

type ProtectedImageProps = {
	src?: string | null;
	localSrc?: string | null;
	alt: string;
	className: string;
	fallback: ReactNode;
	onProtectedSrcNotFound?: () => void;
	onProtectedSrcLoaded?: () => void;
};

export function ProtectedImage({
	src,
	localSrc,
	alt,
	className,
	fallback,
	onProtectedSrcNotFound,
	onProtectedSrcLoaded,
}: ProtectedImageProps) {
	const [failed, setFailed] = useState(false);
	const [imageURL, setImageURL] = useState<string | null>(() =>
		getCachedProtectedImageDataURL(src),
	);

	useEffect(() => {
		setFailed(false);
		if (!src || !isProtectedFileURL(src)) {
			setImageURL(null);
			return;
		}

		const cachedImageURL = getCachedProtectedImageDataURL(src);
		if (cachedImageURL) {
			setImageURL(cachedImageURL);
			onProtectedSrcLoaded?.();
			return;
		}

		let cancelled = false;
		authenticatedFetch(src)
			.then(async (response) => {
				if (!response.ok) throw new Error(`HTTP ${response.status}`);
				return response.blob();
			})
			.then(async (blob) => {
				if (cancelled) return;
				const dataURL = await blobToDataURL(blob);
				if (cancelled) return;
				cacheProtectedImageDataURL(src, dataURL);
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
				if (!cachedImageURL) setFailed(true);
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
	if (isProtectedFileURL(src) && !imageURL) return <>{fallback}</>;

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
