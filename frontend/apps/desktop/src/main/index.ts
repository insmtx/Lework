import { join } from "node:path";
import { electronApp, is, optimizer } from "@electron-toolkit/utils";
import {
	app,
	BrowserWindow,
	ipcMain,
	Menu,
	nativeImage,
	shell,
	Tray,
} from "electron";
import {
	desktopOpenPolicyPdfChannel,
	type DesktopPolicyDocument,
} from "../shared/auto-update";
import { getDesktopUpdateState, registerDesktopAutoUpdate } from "./auto-update";

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let isQuitting = false;

function getPolicyPdfPath(document: DesktopPolicyDocument): string {
	const fileName = document === "terms" ? "terms-of-service.pdf" : "privacy-policy.pdf";
	if (app.isPackaged) {
		return join(process.resourcesPath, fileName);
	}

	return join(__dirname, "../../resources", fileName);
}

function createWindow(): void {
	if (mainWindow && !mainWindow.isDestroyed()) {
		showMainWindow();
		return;
	}

	mainWindow = new BrowserWindow({
		width: 1280,
		height: 800,
		minWidth: 900,
		minHeight: 600,
		show: false,
		autoHideMenuBar: true,
		icon: join(__dirname, "../../resources/icon.png"),
		webPreferences: {
			preload: join(__dirname, "../preload/index.js"),
			sandbox: false,
		},
	});

	mainWindow.on("ready-to-show", () => {
		showMainWindow();
	});

	mainWindow.webContents.setWindowOpenHandler((details) => {
		shell.openExternal(details.url);
		return { action: "deny" };
	});

	mainWindow.on("close", (event) => {
		if (isQuitting) return;

		event.preventDefault();
		hideMainWindow();
	});

	mainWindow.on("closed", () => {
		mainWindow = null;
	});

	if (is.dev && process.env.ELECTRON_RENDERER_URL) {
		mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL);
	} else {
		mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
	}
}

function showMainWindow(): void {
	if (!mainWindow || mainWindow.isDestroyed()) {
		createWindow();
		return;
	}

	if (mainWindow.isMinimized()) mainWindow.restore();
	mainWindow.show();
	mainWindow.focus();
}

function hideMainWindow(): void {
	if (!mainWindow || mainWindow.isDestroyed()) return;

	mainWindow.hide();
}

function createTray(): void {
	if (tray) return;

	const trayIconFile = process.platform === "darwin" ? "tray-icon.png" : "icon.png";
	const icon = nativeImage.createFromPath(join(__dirname, "../../resources", trayIconFile));
	const trayIcon =
		process.platform === "darwin" ? icon.resize({ width: 18, height: 18 }) : icon.resize({ width: 20, height: 20 });

	tray = new Tray(trayIcon);
	tray.setToolTip("Lework");
	tray.on("click", handleTrayClick);
	tray.on("right-click", () => {
		tray?.popUpContextMenu(buildTrayMenu());
	});
}

function handleTrayClick(): void {
	tray?.popUpContextMenu(buildTrayMenu());
}

function buildTrayMenu(): Menu {
	const updateState = getDesktopUpdateState();

	return Menu.buildFromTemplate([
		{
			label: "状态：运行中",
			enabled: false,
		},
		{
			label: `版本：${app.getVersion()}`,
			enabled: false,
		},
		{
			label: `发现新版本：${formatAvailableVersion(updateState.availableVersion || updateState.downloadedVersion)}`,
			enabled: false,
		},
		{ type: "separator" },
		{
			label: "打开 Lework",
			click: showMainWindow,
		},
		{ type: "separator" },
		{
			label: "退出",
			accelerator: process.platform === "darwin" ? "Command+Q" : "Ctrl+Q",
			click: quitApp,
		},
	]);
}

function formatAvailableVersion(version: string | undefined): string {
	if (!version) return "暂无";

	return `${version}（重启服务后生效）`;
}

function quitApp(): void {
	isQuitting = true;
	app.quit();
}

ipcMain.handle(desktopOpenPolicyPdfChannel, async (_event, document: DesktopPolicyDocument) => {
	const result = await shell.openPath(getPolicyPdfPath(document));
	return result === "";
});

app.whenReady().then(() => {
	electronApp.setAppUserModelId("com.leros.desktop");

	app.on("browser-window-created", (_, window) => {
		optimizer.watchWindowShortcuts(window);
	});

	createWindow();
	createTray();
	registerDesktopAutoUpdate();

	app.on("activate", () => {
		if (!mainWindow || mainWindow.isDestroyed()) {
			createWindow();
			return;
		}

		showMainWindow();
	});
});

app.on("window-all-closed", () => {
	if (process.platform !== "darwin") app.quit();
});

app.on("before-quit", () => {
	isQuitting = true;
});
