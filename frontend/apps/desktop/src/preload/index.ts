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
import {
	type DesktopConfigApi,
	desktopSetApiBaseURLChannel,
	desktopTestServerConnectionChannel,
} from "../shared/desktop-config";

const desktopApi: DesktopUpdateApi & DesktopConfigApi = {
	getState: () => ipcRenderer.invoke(desktopUpdateGetStateChannel),
	checkForUpdates: () => ipcRenderer.invoke(desktopUpdateCheckChannel),
	quitAndInstall: () => ipcRenderer.invoke(desktopUpdateRestartChannel),
	openPolicyPdf: (document: DesktopPolicyDocument) =>
		ipcRenderer.invoke(desktopOpenPolicyPdfChannel, document),
	openExternal: (url: string) => ipcRenderer.invoke(desktopOpenExternalChannel, url),
	setApiBaseURL: (url: string) => ipcRenderer.invoke(desktopSetApiBaseURLChannel, url),
	testServerConnection: (url: string) =>
		ipcRenderer.invoke(desktopTestServerConnectionChannel, url),
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
	contextBridge.exposeInMainWorld("leworkDesktop", desktopApi);
} else {
	window.electron = electronAPI;
	window.leworkDesktop = desktopApi;
}
