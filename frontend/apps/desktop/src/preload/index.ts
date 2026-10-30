import { electronAPI } from "@electron-toolkit/preload";
import type { IpcRendererEvent } from "electron";
import { contextBridge, ipcRenderer } from "electron";
import {
	type DesktopPolicyDocument,
	type DesktopUpdateApi,
	type DesktopUpdateState,
	desktopOpenExternalChannel,
	desktopOpenPolicyPdfChannel,
	desktopUpdateCheckChannel,
	desktopUpdateEventChannel,
	desktopUpdateGetStateChannel,
	desktopUpdateRestartChannel,
} from "../shared/auto-update";
import { type DiscoveryApi, discoveryScanChannel } from "../shared/discovery";

const desktopUpdateApi: DesktopUpdateApi = {
	getState: () => ipcRenderer.invoke(desktopUpdateGetStateChannel),
	checkForUpdates: () => ipcRenderer.invoke(desktopUpdateCheckChannel),
	quitAndInstall: () => ipcRenderer.invoke(desktopUpdateRestartChannel),
	openPolicyPdf: (document: DesktopPolicyDocument) =>
		ipcRenderer.invoke(desktopOpenPolicyPdfChannel, document),
	openExternal: (url: string) => ipcRenderer.invoke(desktopOpenExternalChannel, url),
	subscribe: (listener) => {
		const handler = (_event: IpcRendererEvent, state: DesktopUpdateState) => {
			listener(state);
		};

		ipcRenderer.on(desktopUpdateEventChannel, handler);

		return () => {
			ipcRenderer.removeListener(desktopUpdateEventChannel, handler);
		};
	},
};

const discoveryApi: DiscoveryApi = { scan: () => ipcRenderer.invoke(discoveryScanChannel) };

if (process.contextIsolated) {
	contextBridge.exposeInMainWorld("electron", electronAPI);
	contextBridge.exposeInMainWorld("lerosDiscovery", discoveryApi);
	contextBridge.exposeInMainWorld("lerosDesktop", desktopUpdateApi);
} else {
	window.electron = electronAPI;
	window.lerosDiscovery = discoveryApi;
	window.lerosDesktop = desktopUpdateApi;
}
