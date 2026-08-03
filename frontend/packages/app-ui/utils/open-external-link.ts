type DesktopExternalLinkApi = {
	openExternal?: (url: string) => Promise<boolean>;
};

export function isExternalHttpLink(href: string | undefined): href is string {
	return typeof href === "string" && (href.startsWith("http://") || href.startsWith("https://"));
}

function getDesktopExternalLinkApi(): DesktopExternalLinkApi | null {
	if (typeof window === "undefined") {
		return null;
	}

	return (window as Window & { lerosDesktop?: DesktopExternalLinkApi }).lerosDesktop ?? null;
}

export function openExternalLink(href: string): void {
	const desktopApi = getDesktopExternalLinkApi();
	if (desktopApi?.openExternal) {
		void desktopApi.openExternal(href);
		return;
	}

	window.open(href, "_blank", "noopener,noreferrer");
}
