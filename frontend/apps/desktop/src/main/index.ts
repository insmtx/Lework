import { join } from "node:path";
import { electronApp, is, optimizer } from "@electron-toolkit/utils";
import { app, BrowserWindow, ipcMain, Menu, nativeImage, shell, Tray } from "electron";
import {
	type DesktopPolicyDocument,
	desktopOpenExternalChannel,
	desktopOpenPolicyPdfChannel,
} from "../shared/auto-update";
import {
	isAppQuitPrepared,
	isAppQuitting,
	markAppQuitting,
	prepareForAppQuit,
	prepareWindowForHide,
} from "./app-lifecycle";
import { getDesktopUpdateState, registerDesktopAutoUpdate } from "./auto-update";
import { isProductionDevToolsShortcut } from "./devtools-shortcut";
import { shouldOpenExternalUrl } from "./external-navigation";
import { configureTrayInteractions } from "./tray-interactions";

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let mainWindowHideInProgress = false;

// 中文注释：银河麒麟/UKUI 主要通过 X11 WM_CLASS 将运行窗口关联到 .desktop 启动器。
// 显式固定 class，避免不同 Electron/Chromium 版本使用产品名或可执行文件名导致匹配失败。
if (process.platform === "linux") {
	app.commandLine.appendSwitch("class", "leros-desktop");
}

const gotSingleInstanceLock = app.requestSingleInstanceLock();

if (!gotSingleInstanceLock) {
	markAppQuitting();
	app.quit();
}

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
		// mac 沉浸式一体化标题栏：隐藏系统标题栏，红绿灯按钮内嵌到侧栏左上角。
		// trafficLightPosition 与侧栏品牌行 padding-top 配合，让红绿灯落在 Logo 行左侧空白区。
		...(process.platform === "darwin"
			? {
					titleBarStyle: "hiddenInset" as const,
					trafficLightPosition: { x: 16, y: 14 },
				}
			: {}),
		icon: join(
			__dirname,
			"../../resources",
			process.platform === "darwin" ? "icon-mac.png" : "icon.png",
		),
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

	mainWindow.webContents.on("will-navigate", (event, url) => {
		const devRendererUrl = is.dev ? process.env.ELECTRON_RENDERER_URL : undefined;
		if (!shouldOpenExternalUrl(url, devRendererUrl)) {
			return;
		}

		event.preventDefault();
		void shell.openExternal(url);
	});

	mainWindow.webContents.on("before-input-event", (event, input) => {
		if (!app.isPackaged || !isProductionDevToolsShortcut(input, process.platform)) return;

		event.preventDefault();
		openMainWindowDevTools();
	});

	mainWindow.on("close", (event) => {
		if (isAppQuitting()) return;

		event.preventDefault();
		void hideMainWindow();
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

function openMainWindowDevTools(): void {
	if (mainWindow && !mainWindow.isDestroyed() && !mainWindow.webContents.isDevToolsOpened()) {
		mainWindow.webContents.openDevTools({ mode: "detach" });
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

function focusMainWindow(): void {
	showMainWindow();

	if (process.platform === "win32" && mainWindow && !mainWindow.isDestroyed()) {
		mainWindow.setAlwaysOnTop(true);
		mainWindow.setAlwaysOnTop(false);
	}
}

async function hideMainWindow(): Promise<void> {
	if (!mainWindow || mainWindow.isDestroyed()) return;
	if (mainWindowHideInProgress) return;

	mainWindowHideInProgress = true;
	const window = mainWindow;
	try {
		await prepareWindowForHide(window);
		if (!window.isDestroyed()) {
			window.hide();
		}
	} finally {
		mainWindowHideInProgress = false;
	}
}

function createTray(): void {
	if (tray) return;

	const trayIconFile = process.platform === "darwin" ? "tray-icon.png" : "icon.png";
	const icon = nativeImage.createFromPath(join(__dirname, "../../resources", trayIconFile));
	const trayIcon =
		process.platform === "darwin"
			? icon.resize({ width: 18, height: 18 })
			: icon.resize({ width: 20, height: 20 });

	tray = new Tray(trayIcon);
	tray.setToolTip("Lework");
	configureTrayInteractions(tray, process.platform, buildTrayMenu, showMainWindow);
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

async function quitApp(): Promise<void> {
	markAppQuitting();
	await prepareForAppQuit();
	app.quit();
}

ipcMain.handle(desktopOpenPolicyPdfChannel, async (_event, document: DesktopPolicyDocument) => {
	const result = await shell.openPath(getPolicyPdfPath(document));
	return result === "";
});

ipcMain.handle(desktopOpenExternalChannel, async (_event, url: unknown) => {
	if (typeof url !== "string" || !shouldOpenExternalUrl(url)) {
		return false;
	}

	await shell.openExternal(url);
	return true;
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

app.on("second-instance", () => {
	focusMainWindow();
});

app.on("window-all-closed", () => {
	if (process.platform !== "darwin") app.quit();
});

app.on("before-quit", (event) => {
	markAppQuitting();

	if (isAppQuitPrepared()) {
		return;
	}

	event.preventDefault();
	void prepareForAppQuit().finally(() => {
		app.quit();
	});
});
