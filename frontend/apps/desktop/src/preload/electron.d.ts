import { electronAPI } from "@electron-toolkit/preload";
import type { DesktopUpdateApi } from "../shared/auto-update";
import type { DesktopConfigApi } from "../shared/desktop-config";

declare global {
	interface Window {
		electron: typeof electronAPI;
		leworkDesktop: DesktopUpdateApi & DesktopConfigApi;
	}
}
