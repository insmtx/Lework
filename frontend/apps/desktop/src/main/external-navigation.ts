export function isExternalHttpUrl(url: string): boolean {
	return url.startsWith("http://") || url.startsWith("https://");
}

export function shouldOpenExternalUrl(url: string, devRendererUrl?: string): boolean {
	if (!isExternalHttpUrl(url)) {
		return false;
	}

	if (!devRendererUrl) {
		return true;
	}

	try {
		return new URL(url).origin !== new URL(devRendererUrl).origin;
	} catch {
		return true;
	}
}
