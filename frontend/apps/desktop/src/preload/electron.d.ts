import { electronAPI } from "@electron-toolkit/preload";
import type { DesktopUpdateApi } from "../shared/auto-update";

declare global {
	interface Window {
		electron: typeof electronAPI;
		leworkDesktop: DesktopUpdateApi;
	}
}
