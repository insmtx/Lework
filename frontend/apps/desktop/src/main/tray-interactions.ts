import type { Menu, Tray } from "electron";

/**
 * Configure platform-specific tray interactions.
 *
 * Linux StatusNotifierItem implementations own context-menu presentation, so
 * they must receive a menu through setContextMenu instead of popUpContextMenu.
 */
export function configureTrayInteractions(
	tray: Tray,
	platform: NodeJS.Platform,
	buildMenu: () => Menu,
	showWindow: () => void,
): void {
	if (platform === "linux") {
		tray.setContextMenu(buildMenu());
		tray.on("click", showWindow);
		return;
	}

	const showMenu = () => {
		tray.popUpContextMenu(buildMenu());
	};

	tray.on("click", showMenu);
	tray.on("right-click", showMenu);
}
