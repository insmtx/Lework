export const desktopDeepLinkScheme = "leros";

/**
 * 从启动参数中提取可用于切换桌面端服务地址的深链，避免将其他命令行参数误作链接。
 */
export function extractDesktopServerURL(commandLine: readonly string[]): string | null {
	const deepLink = commandLine.find((argument) =>
		argument.startsWith(`${desktopDeepLinkScheme}://`),
	);
	if (!deepLink) return null;

	try {
		const url = new URL(deepLink);
		if (url.protocol !== `${desktopDeepLinkScheme}:` || url.hostname !== "open") {
			return null;
		}

		const serverURL = url.searchParams.get("server");
		if (!serverURL || serverURL.length > 2048) return null;

		const server = new URL(serverURL);
		if (
			(server.protocol !== "http:" && server.protocol !== "https:") ||
			server.username ||
			server.password ||
			server.search ||
			server.hash
		) {
			return null;
		}

		return server.toString();
	} catch {
		return null;
	}
}
