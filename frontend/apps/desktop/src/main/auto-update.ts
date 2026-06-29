import {
	type AppUpdater,
	type UpdateDownloadedEvent,
	type UpdateInfo,
	MacUpdater,
	NsisUpdater,
} from "electron-updater";
import { app, BrowserWindow, ipcMain } from "electron";
import {
	type DesktopUpdateState,
	desktopUpdateCheckChannel,
	desktopUpdateEventChannel,
	desktopUpdateGetStateChannel,
	desktopUpdateRestartChannel,
} from "../shared/auto-update";
import { markAppQuitting } from "./app-lifecycle";

const autoUpdateIntervalMs = 30 * 60 * 1000;
const initialAutoUpdateDelayMs = 1 * 1000;
const desktopUpdateBaseURL = "https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/stable";
const enableDevAutoUpdate = !app.isPackaged;

let updateState: DesktopUpdateState = createState({
	phase: "idle",
	message: "准备检查更新",
	canCheck: false,
	canRestart: false,
});

let updateHandlersRegistered = false;
let autoUpdateTimer: NodeJS.Timeout | null = null;
let updaterInstance: AppUpdater | null = null;

type CheckForUpdatesOptions = {
	manual?: boolean;
};

function createState(overrides: Partial<DesktopUpdateState>): DesktopUpdateState {
	return {
		currentVersion: app.getVersion(),
		phase: "idle",
		message: "",
		canCheck: true,
		canRestart: false,
		...overrides,
	};
}

function broadcastState() {
	for (const window of BrowserWindow.getAllWindows()) {
		if (!window.isDestroyed()) {
			window.webContents.send(desktopUpdateEventChannel, updateState);
		}
	}
}

function setState(overrides: Partial<DesktopUpdateState>) {
	updateState = {
		...updateState,
		...overrides,
		currentVersion: app.getVersion(),
	};
	broadcastState();
}

function getReleaseNotes(info: UpdateInfo | UpdateDownloadedEvent): string | undefined {
	if (Array.isArray(info.releaseNotes)) {
		return info.releaseNotes
			.map((item) => {
				if (typeof item === "string") {
					return item;
				}

				return `${item.version ?? ""} ${item.note ?? ""}`.trim();
			})
			.filter(Boolean)
			.join("\n\n");
	}

	if (typeof info.releaseNotes === "string") {
		return info.releaseNotes;
	}

	return undefined;
}

function markUnsupported(message: string) {
	setState({
		phase: "unsupported",
		message,
		canCheck: false,
		canRestart: false,
		progressPercent: undefined,
	});
}

function canUseAutoUpdate(): boolean {
	return (app.isPackaged || enableDevAutoUpdate) && (process.platform === "darwin" || process.platform === "win32");
}

function getUpdateFeedURL(): string {
	return `${desktopUpdateBaseURL}/${process.platform}/${process.arch}`;
}

function getUpdater(): AppUpdater | null {
	if (updaterInstance) {
		return updaterInstance;
	}

	if (process.platform === "darwin") {
		updaterInstance = new MacUpdater({
			provider: "generic",
			url: getUpdateFeedURL(),
		});
	} else if (process.platform === "win32") {
		updaterInstance = new NsisUpdater({
			provider: "generic",
			url: getUpdateFeedURL(),
		});
	} else {
		return null;
	}

	updaterInstance.autoDownload = true;
	updaterInstance.autoInstallOnAppQuit = true;
	updaterInstance.forceDevUpdateConfig = enableDevAutoUpdate;

	return updaterInstance;
}

function registerAutoUpdaterEvents() {
	if (updateHandlersRegistered) {
		return;
	}

	const updater = getUpdater();
	if (!updater) {
		return;
	}

	updateHandlersRegistered = true;

	updater.on("checking-for-update", () => {
		setState({
			phase: "checking",
			message: "正在检查更新",
			canCheck: false,
			canRestart: false,
			progressPercent: undefined,
		});
	});

	updater.on("update-available", (info) => {
		setState({
			phase: "available",
			message: "发现新版本，开始后台下载",
			availableVersion: info.version,
			releaseDate: info.releaseDate,
			releaseNotes: getReleaseNotes(info),
			canCheck: false,
			canRestart: false,
			lastCheckedAt: new Date().toISOString(),
		});
	});

	updater.on("update-not-available", () => {
		setState({
			phase: "up-to-date",
			message: "当前已经是最新版本",
			availableVersion: undefined,
			downloadedVersion: undefined,
			releaseDate: undefined,
			releaseNotes: undefined,
			progressPercent: undefined,
			canCheck: true,
			canRestart: false,
			lastCheckedAt: new Date().toISOString(),
		});
	});

	updater.on("download-progress", (progress) => {
		setState({
			phase: "downloading",
			message: `正在下载更新 ${Math.round(progress.percent)}%`,
			progressPercent: progress.percent,
			canCheck: false,
			canRestart: false,
		});
	});

	updater.on("update-downloaded", (info) => {
		setState({
			phase: "downloaded",
			message: "更新已下载完成，重启后安装",
			downloadedVersion: info.version,
			availableVersion: info.version,
			releaseDate: info.releaseDate,
			releaseNotes: getReleaseNotes(info),
			progressPercent: 100,
			canCheck: true,
			canRestart: true,
			lastCheckedAt: new Date().toISOString(),
		});
	});

	updater.on("error", (error) => {
		setState({
			phase: "error",
			message: error?.message || "更新失败，请稍后重试",
			canCheck: true,
			canRestart: false,
			progressPercent: undefined,
			lastCheckedAt: new Date().toISOString(),
		});
	});
}

async function checkForUpdates(options: CheckForUpdatesOptions = {}): Promise<DesktopUpdateState> {
	if (!options.manual && updateState.phase === "downloaded" && updateState.downloadedVersion) {
		return updateState;
	}

	if (!canUseAutoUpdate()) {
		markUnsupported("自动更新仅在已安装的 macOS / Windows 版本中可用");
		return updateState;
	}

	const updater = getUpdater();
	if (!updater) {
		markUnsupported("当前平台不支持自动更新");
		return updateState;
	}

	try {
		await updater.checkForUpdates();
	} catch (error) {
		const message = error instanceof Error ? error.message : "检查更新失败";
		setState({
			phase: "error",
			message,
			canCheck: true,
			canRestart: false,
			lastCheckedAt: new Date().toISOString(),
		});
	}

	return updateState;
}

function scheduleAutoUpdateChecks() {
	if (autoUpdateTimer) {
		clearInterval(autoUpdateTimer);
	}

	setTimeout(() => {
		void checkForUpdates();
	}, initialAutoUpdateDelayMs);

	autoUpdateTimer = setInterval(() => {
		void checkForUpdates();
	}, autoUpdateIntervalMs);
}

export function registerDesktopAutoUpdate() {
	if (canUseAutoUpdate()) {
		registerAutoUpdaterEvents();
		setState({
			phase: "idle",
			message: enableDevAutoUpdate
				? `开发环境可手动检查更新 (${process.platform}/${process.arch})`
				: `已启用自动更新 (${process.platform}/${process.arch})`,
			canCheck: true,
			canRestart: false,
		});
		scheduleAutoUpdateChecks();
	} else {
		markUnsupported("开发环境不执行自动更新，请使用安装包验证");
	}

	ipcMain.handle(desktopUpdateGetStateChannel, () => updateState);
	ipcMain.handle(desktopUpdateCheckChannel, async () => {
		return checkForUpdates({ manual: true });
	});
	ipcMain.handle(desktopUpdateRestartChannel, () => {
		if (!updateState.canRestart) {
			return false;
		}

		const updater = getUpdater();
		if (!updater) {
			return false;
		}

		markAppQuitting();
		updater.quitAndInstall();
		return true;
	});
}

export function getDesktopUpdateState(): DesktopUpdateState {
	return updateState;
}
