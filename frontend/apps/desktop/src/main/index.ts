import { join } from "node:path";
import { electronApp, is, optimizer } from "@electron-toolkit/utils";
import { app, BrowserWindow, dialog, ipcMain, Menu, nativeImage, shell, Tray } from "electron";
import {
	type DesktopPolicyDocument,
	desktopOpenExternalChannel,
	desktopOpenPolicyPdfChannel,
} from "../shared/auto-update";
import {
	desktopSetApiBaseURLChannel,
	desktopTestServerConnectionChannel,
} from "../shared/desktop-config";
import {
	isAppQuitPrepared,
	isAppQuitting,
	markAppQuitting,
	prepareForAppQuit,
	prepareWindowForHide,
} from "./app-lifecycle";
import { getDesktopUpdateState, registerDesktopAutoUpdate } from "./auto-update";
import { readDesktopConfig, writeDesktopConfig } from "./desktop-config";
import { isProductionDevToolsShortcut } from "./devtools-shortcut";
import { shouldOpenExternalUrl } from "./external-navigation";
import { LOCAL_ORIGIN_PORT, startLocalOrigin, type LocalOrigin } from "./local-origin";
import {
	buildRestoreScript,
	parseLocalStorageSnapshot,
	runBestEffortMigration,
} from "./local-storage-migration";
import { configureTrayInteractions } from "./tray-interactions";

const DEFAULT_API_UPSTREAM = "http://localhost:8080/v1";
const DEFAULT_CONTROL_PLANE_URL = "http://127.0.0.1:8787";
const CONNECTION_TEST_TIMEOUT_MS = 8000;

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let mainWindowHideInProgress = false;
let localOrigin: LocalOrigin | null = null;
let configDir = "";
// 旧 file:// origin 的 localStorage 快照：主窗口第一次加载后写回再刷新。
let pendingRestore: Record<string, string> | null = null;

// 中文注释：银河麒麟/UKUI 主要通过 X11 WM_CLASS 将运行窗口关联到 .desktop 启动器。
// 显式固定 class，避免不同 Electron/Chromium 版本使用产品名或可执行文件名导致匹配失败。
if (process.platform === "linux") {
	app.commandLine.appendSwitch("class", "lework-desktop");
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
		// 有旧数据待恢复时先保持隐藏，恢复 + reload 完成后再显示，避免闪一下旧状态。
		if (pendingRestore) return;
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
	} else if (localOrigin) {
		mainWindow.loadURL(localOrigin.origin);
	} else {
		mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
	}

	if (pendingRestore) {
		const entries = pendingRestore;
		pendingRestore = null;
		const window = mainWindow;
		window.webContents.once("did-finish-load", () => {
			void window.webContents
				.executeJavaScript(buildRestoreScript(entries))
				.catch((error) => console.error("[desktop] 恢复旧 localStorage 失败", error))
				.finally(() => {
					window.webContents.once("did-finish-load", () => showMainWindow());
					window.webContents.reload();
				});
		});
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
	await shutdownLocalOrigin();
	app.quit();
}

function isHttpURL(value: string): boolean {
	try {
		const url = new URL(value);
		return url.protocol === "http:" || url.protocol === "https:";
	} catch {
		return false;
	}
}

function normalizeServerURL(value: string): string {
	return value.trim().replace(/\/+$/, "");
}

function resolveUpstreams(config: { apiBaseURL?: string; controlPlaneBaseURL?: string }): {
	apiBaseURL: string;
	controlPlaneBaseURL: string;
} {
	return {
		apiBaseURL: config.apiBaseURL ?? process.env.LEWORK_API_UPSTREAM ?? DEFAULT_API_UPSTREAM,
		controlPlaneBaseURL:
			process.env.LEWORK_CONTROL_PLANE_URL ??
			config.controlPlaneBaseURL ??
			DEFAULT_CONTROL_PLANE_URL,
	};
}

async function readOldLocalStorage(): Promise<Record<string, string>> {
	const window = new BrowserWindow({
		show: false,
		webPreferences: {
			preload: join(__dirname, "../preload/index.js"),
			sandbox: false,
		},
	});
	try {
		await window.loadFile(join(__dirname, "../renderer/index.html"));
		const raw = await window.webContents.executeJavaScript("JSON.stringify({ ...localStorage })");
		return parseLocalStorageSnapshot(typeof raw === "string" ? raw : "");
	} catch (error) {
		console.error("[desktop] 读取旧 localStorage 失败", error);
		return {};
	} finally {
		if (!window.isDestroyed()) window.destroy();
	}
}

async function migrateFromFileOrigin(): Promise<void> {
	const config = readDesktopConfig(configDir);
	if (config.migratedFromFileOrigin === true) return;

	const entries = await readOldLocalStorage();
	const storedServerURL = entries["lework-server-base-url"];
	if (storedServerURL && isHttpURL(storedServerURL)) {
		const apiBaseURL = normalizeServerURL(storedServerURL);
		writeDesktopConfig(configDir, { apiBaseURL });
		localOrigin?.setUpstreams({ apiBaseURL });
	}
	if (Object.keys(entries).length > 0) pendingRestore = entries;
	writeDesktopConfig(configDir, { migratedFromFileOrigin: true });
}

async function shutdownLocalOrigin(): Promise<void> {
	if (!localOrigin) return;
	const origin = localOrigin;
	localOrigin = null;
	await origin.close();
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

ipcMain.handle(desktopSetApiBaseURLChannel, async (_event, raw: unknown) => {
	if (typeof raw !== "string") return;
	const trimmed = raw.trim();
	const apiBaseURL = trimmed
		? normalizeServerURL(trimmed)
		: resolveUpstreams(readDesktopConfig(configDir)).apiBaseURL;
	if (trimmed && !isHttpURL(apiBaseURL)) {
		throw new Error("服务地址必须是 http(s) 地址");
	}
	writeDesktopConfig(configDir, { apiBaseURL });
	localOrigin?.setUpstreams({ apiBaseURL });
});

ipcMain.handle(desktopTestServerConnectionChannel, async (_event, raw: unknown) => {
	if (typeof raw !== "string" || raw.trim() === "") {
		throw new Error("服务地址不能为空");
	}
	const baseURL = normalizeServerURL(raw);
	if (!isHttpURL(baseURL)) {
		throw new Error("服务地址必须是 http(s) 地址");
	}

	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), CONNECTION_TEST_TIMEOUT_MS);
	try {
		const response = await fetch(`${baseURL}/GlobalConfig`, {
			headers: { Accept: "application/json" },
			signal: controller.signal,
		});
		if (!response.ok) {
			throw new Error(`服务返回异常状态（HTTP ${response.status}）`);
		}
		const payload: unknown = await response.json();
		const data =
			typeof payload === "object" && payload !== null && "data" in payload
				? (payload as { data?: unknown }).data
				: undefined;
		const edition =
			typeof data === "object" && data !== null && "edition" in data
				? (data as { edition?: unknown }).edition
				: undefined;
		if (edition !== "oss" && edition !== "enterprise") {
			throw new Error("服务响应格式不正确，请确认连接的是 Lework 后端");
		}
		return baseURL;
	} catch (error) {
		if (error instanceof Error && error.name === "AbortError") {
			throw new Error("连接超时，请检查服务地址和网络");
		}
		throw error;
	} finally {
		clearTimeout(timeout);
	}
});

app.whenReady().then(async () => {
	electronApp.setAppUserModelId("com.lework.desktop");

	app.on("browser-window-created", (_, window) => {
		optimizer.watchWindowShortcuts(window);
	});

	configDir = app.getPath("userData");

	if (!is.dev) {
		try {
			localOrigin = await startLocalOrigin({
				rootDir: join(__dirname, "../renderer"),
				...resolveUpstreams(readDesktopConfig(configDir)),
				port: LOCAL_ORIGIN_PORT,
			});
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error);
			dialog.showErrorBox(
				"Lework 无法启动",
				`本地渲染源（127.0.0.1:${LOCAL_ORIGIN_PORT}）启动失败：${message}\n\n请关闭占用该端口的程序后重试。`,
			);
			markAppQuitting();
			app.quit();
			return;
		}
		await runBestEffortMigration(migrateFromFileOrigin);
	}

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
	void prepareForAppQuit()
		.then(() => shutdownLocalOrigin())
		.finally(() => {
			app.quit();
		});
});
