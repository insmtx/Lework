import { electronAPI } from "@electron-toolkit/preload";
import type { IpcRendererEvent } from "electron";
import { contextBridge, ipcRenderer } from "electron";
import {
	type DesktopPolicyDocument,
	type DesktopUpdateApi,
	desktopOpenPolicyPdfChannel,
	type DesktopUpdateState,
	desktopUpdateCheckChannel,
	desktopUpdateEventChannel,
	desktopUpdateGetStateChannel,
	desktopUpdateRestartChannel,
} from "../shared/auto-update";

const desktopUpdateApi: DesktopUpdateApi = {
	getState: () => ipcRenderer.invoke(desktopUpdateGetStateChannel),
	checkForUpdates: () => ipcRenderer.invoke(desktopUpdateCheckChannel),
	quitAndInstall: () => ipcRenderer.invoke(desktopUpdateRestartChannel),
	openPolicyPdf: (document: DesktopPolicyDocument) =>
		ipcRenderer.invoke(desktopOpenPolicyPdfChannel, document),
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

if (process.contextIsolated) {
	contextBridge.exposeInMainWorld("electron", electronAPI);
	contextBridge.exposeInMainWorld("lerosDesktop", desktopUpdateApi);
} else {
	window.electron = electronAPI;
	window.lerosDesktop = desktopUpdateApi;
}
