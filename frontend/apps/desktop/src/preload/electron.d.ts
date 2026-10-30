import { electronAPI } from "@electron-toolkit/preload";
import type { DesktopUpdateApi } from "../shared/auto-update";
import type { DiscoveryApi } from "../shared/discovery";

declare global {
	interface Window {
		lerosDiscovery: DiscoveryApi;
		electron: typeof electronAPI;
		lerosDesktop: DesktopUpdateApi;
	}
}
