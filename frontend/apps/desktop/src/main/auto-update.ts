import { app, BrowserWindow, ipcMain, net } from "electron";
import {
	type AppUpdater,
	MacUpdater,
	NsisUpdater,
	type UpdateDownloadedEvent,
	type UpdateInfo,
} from "electron-updater";
import {
	type DesktopUpdateState,
	desktopUpdateCheckChannel,
	desktopUpdateEventChannel,
	desktopUpdateGetStateChannel,
	desktopUpdateRestartChannel,
} from "../shared/auto-update";
import { markAppQuitting, prepareForAppQuit } from "./app-lifecycle";
import { isVersionNewer, type LinuxUpdateMetadata, parseLinuxUpdateMetadata } from "./linux-update";
import {
	DESKTOP_UPDATE_BASE_URL,
	desktopTarget,
	feedUrl,
	latestTxtUrl,
	metadataUrl,
	parseLatestVersion,
} from "./update-source";

const autoUpdateIntervalMs = 30 * 60 * 1000;
const initialAutoUpdateDelayMs = 1 * 1000;
const enableDevAutoUpdate = !app.isPackaged;
const privateDeploymentUnsupportedMessage = "私有化版本请通过离线安装包更新";
const unsupportedPlatformMessage = "当前平台不支持自动更新";

function isPrivateDeploymentBuild(): boolean {
	return process.env.LEWORK_DEPLOYMENT_MODE === "private";
}

let updateState: DesktopUpdateState = createState({
	phase: "idle",
	message: "准备检查更新",
	canCheck: false,
	canRestart: false,
});

let updateHandlersRegistered = false;
let autoUpdateTimer: NodeJS.Timeout | null = null;
let updaterInstance: AppUpdater | null = null;

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
		downloadUrl: undefined,
	});
}

function setUpToDate(lastCheckedAt: string) {
	setState({
		phase: "up-to-date",
		message: "当前已经是最新版本",
		availableVersion: undefined,
		downloadedVersion: undefined,
		downloadUrl: undefined,
		releaseDate: undefined,
		releaseNotes: undefined,
		progressPercent: undefined,
		canCheck: true,
		canRestart: false,
		lastCheckedAt,
	});
}

function setUpdateError(message: string) {
	// 已下载完成的更新不能因为一次检查失败就丢掉，否则「重启安装」入口会消失，
	// 用户只能等下一次成功检查并重新下载。
	if (updateState.phase === "downloaded" && updateState.downloadedVersion) {
		setState({
			phase: "downloaded",
			message,
			progressPercent: 100,
			canCheck: true,
			canRestart: true,
			lastCheckedAt: new Date().toISOString(),
		});
		return;
	}

	setState({
		phase: "error",
		message,
		availableVersion: undefined,
		downloadedVersion: undefined,
		downloadUrl: undefined,
		releaseDate: undefined,
		releaseNotes: undefined,
		progressPercent: undefined,
		canCheck: true,
		canRestart: false,
		lastCheckedAt: new Date().toISOString(),
	});
}

// 第一段：只读 LATEST.txt 拿到远端最新稳定版本。没有新版本时不访问版本目录，
// 避免 COS 上只有版本号、产物尚未上传时把 404 当成错误。
async function fetchLatestReleaseVersion(): Promise<string> {
	const response = await net.fetch(latestTxtUrl(DESKTOP_UPDATE_BASE_URL), {
		cache: "no-store",
		signal: AbortSignal.timeout(15_000),
	});
	if (!response.ok) {
		throw new Error(`读取版本信息失败（HTTP ${response.status}）`);
	}

	// LATEST.txt 允许前导 v，但 COS 目录名不带 v，拼 URL 前必须归一化，
	// 否则会静默 404 并永久显示「已是最新版本」。
	return parseLatestVersion(await response.text()).replace(/^v/i, "");
}

// latest-linux.yml 的 path 通常是 AppImage（x64 写作 x86_64）；deb 与它共用
// electron-builder.yml 的 artifactName 模板，只需换成 deb 的架构写法与扩展名。
const linuxDebArchNames: Record<string, string> = {
	x86_64: "amd64",
	x64: "amd64",
	ia32: "i386",
	arm64: "arm64",
	armv7l: "armv7l",
};

function linuxDebFileName(metadata: LinuxUpdateMetadata): string {
	// path 来自远端 YAML，只接受纯文件名；带目录或协议的内容一律回退到推导名，
	// 避免把「下载安装包」菜单变成打开任意站点。
	const artifact = metadata.path?.trim();
	if (!artifact || artifact.includes("/") || artifact.includes("\\")) {
		return `Lework-${metadata.version}-linux-amd64.deb`;
	}
	if (/\.deb$/i.test(artifact)) {
		return artifact;
	}

	const base = artifact.replace(/\.[^./]+$/, "");
	const arch = base.match(/(x86_64|x64|ia32|arm64|armv7l)$/)?.[1];
	if (!arch) {
		return `Lework-${metadata.version}-linux-amd64.deb`;
	}

	return `${base.slice(0, -arch.length)}${linuxDebArchNames[arch] ?? arch}.deb`;
}

// Linux 不走 electron-updater，只把同目录的 .deb 下载地址交给界面用系统浏览器打开。
function linuxDebUrl(feed: string, metadata: LinuxUpdateMetadata): string {
	return new URL(linuxDebFileName(metadata), `${feed}/`).toString();
}

function getUpdater(): AppUpdater | null {
	if (updaterInstance) {
		return updaterInstance;
	}

	// 这里只填 COS 基址占位；真正使用的版本目录由 checkForUpdates 里的 setFeedURL 决定。
	if (process.platform === "darwin") {
		updaterInstance = new MacUpdater({
			provider: "generic",
			url: DESKTOP_UPDATE_BASE_URL,
		});
	} else if (process.platform === "win32") {
		updaterInstance = new NsisUpdater({
			provider: "generic",
			url: DESKTOP_UPDATE_BASE_URL,
		});
	} else {
		return null;
	}

	updaterInstance.autoDownload = true;
	updaterInstance.autoInstallOnAppQuit = true;
	updaterInstance.forceDevUpdateConfig = enableDevAutoUpdate;

	return updaterInstance;
}

// 第二段（macOS / Windows）：确认版本目录里已经有本平台产物，再让 electron-updater
// 走版本目录检查；产物尚未上传（非 2xx）视为本次没有更新，保持静默。
async function checkNativeUpdate(
	feed: string,
	latestMetadataUrl: string,
): Promise<DesktopUpdateState> {
	const response = await net.fetch(latestMetadataUrl, {
		method: "HEAD",
		cache: "no-store",
		signal: AbortSignal.timeout(15_000),
	});
	if (!response.ok) {
		setUpToDate(new Date().toISOString());
		return updateState;
	}

	const updater = getUpdater();
	if (!updater) {
		markUnsupported(unsupportedPlatformMessage);
		return updateState;
	}

	updater.setFeedURL({ provider: "generic", url: feed });
	const result = await updater.checkForUpdates();
	if (!result) {
		// updater 未激活（例如开发环境缺少配置）时不会触发任何事件，回落避免卡在 checking。
		setUpToDate(new Date().toISOString());
	}

	return updateState;
}

// 第二段（Linux）：electron-updater 不支持 deb 自更新，只解析元数据并给出 .deb 下载地址。
async function checkLinuxUpdate(
	feed: string,
	latestMetadataUrl: string,
): Promise<DesktopUpdateState> {
	const response = await net.fetch(latestMetadataUrl, {
		cache: "no-store",
		signal: AbortSignal.timeout(15_000),
	});
	if (!response.ok) {
		setUpToDate(new Date().toISOString());
		return updateState;
	}

	const metadata = parseLinuxUpdateMetadata(await response.text());
	const lastCheckedAt = new Date().toISOString();
	if (!isVersionNewer(metadata.version, app.getVersion())) {
		setUpToDate(lastCheckedAt);
		return updateState;
	}

	setState({
		phase: "available",
		message: `发现新版本 v${metadata.version}，请下载 .deb 安装包后手动安装`,
		availableVersion: metadata.version,
		downloadedVersion: undefined,
		downloadUrl: linuxDebUrl(feed, metadata),
		releaseDate: metadata.releaseDate,
		releaseNotes: undefined,
		progressPercent: undefined,
		canCheck: true,
		canRestart: false,
		lastCheckedAt,
	});

	return updateState;
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
		const downloadedVersion = updateState.downloadedVersion;
		if (downloadedVersion && downloadedVersion === info.version) {
			setState({
				phase: "downloaded",
				message: "更新已下载完成，重启后安装",
				availableVersion: info.version,
				releaseDate: info.releaseDate,
				releaseNotes: getReleaseNotes(info),
				progressPercent: 100,
				canCheck: true,
				canRestart: true,
				lastCheckedAt: new Date().toISOString(),
			});
			return;
		}

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
			downloadUrl: undefined,
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

export async function checkDesktopUpdates(): Promise<DesktopUpdateState> {
	if (isPrivateDeploymentBuild()) {
		return updateState;
	}

	const target = desktopTarget(process.platform, process.arch);
	if (!target) {
		markUnsupported(unsupportedPlatformMessage);
		return updateState;
	}

	setState({
		phase: "checking",
		message: "正在检查更新",
		canCheck: false,
		canRestart: false,
		progressPercent: undefined,
		downloadUrl: undefined,
	});

	try {
		const latestVersion = await fetchLatestReleaseVersion();
		if (!isVersionNewer(latestVersion, app.getVersion())) {
			setUpToDate(new Date().toISOString());
			return updateState;
		}

		const feed = feedUrl(DESKTOP_UPDATE_BASE_URL, latestVersion, target);
		const latestMetadataUrl = metadataUrl(DESKTOP_UPDATE_BASE_URL, latestVersion, target);

		if (process.platform === "linux") {
			return await checkLinuxUpdate(feed, latestMetadataUrl);
		}

		return await checkNativeUpdate(feed, latestMetadataUrl);
	} catch (error) {
		setUpdateError(error instanceof Error ? error.message : "检查更新失败");
		return updateState;
	}
}

function scheduleAutoUpdateChecks() {
	if (autoUpdateTimer) {
		clearInterval(autoUpdateTimer);
	}

	setTimeout(() => {
		void checkDesktopUpdates();
	}, initialAutoUpdateDelayMs);

	autoUpdateTimer = setInterval(() => {
		void checkDesktopUpdates();
	}, autoUpdateIntervalMs);
}

export function registerDesktopAutoUpdate() {
	const target = desktopTarget(process.platform, process.arch);
	if (isPrivateDeploymentBuild()) {
		markUnsupported(privateDeploymentUnsupportedMessage);
	} else if (!target) {
		markUnsupported(unsupportedPlatformMessage);
	} else {
		if (process.platform === "linux") {
			setState({
				phase: "idle",
				message: "Linux 可检查新版本，.deb 安装包需要手动安装",
				canCheck: true,
				canRestart: false,
			});
		} else {
			registerAutoUpdaterEvents();
			setState({
				phase: "idle",
				message: enableDevAutoUpdate
					? `开发环境可手动检查更新 (${process.platform}/${process.arch})`
					: `已启用自动更新 (${process.platform}/${process.arch})`,
				canCheck: true,
				canRestart: false,
			});
		}
		scheduleAutoUpdateChecks();
	}

	ipcMain.handle(desktopUpdateGetStateChannel, () => updateState);
	ipcMain.handle(desktopUpdateCheckChannel, async () => {
		return checkDesktopUpdates();
	});
	ipcMain.handle(desktopUpdateRestartChannel, async () => {
		if (!updateState.canRestart) {
			return false;
		}

		const updater = getUpdater();
		if (!updater) {
			return false;
		}

		markAppQuitting();
		await prepareForAppQuit();
		updater.quitAndInstall();
		return true;
	});
}

export function getDesktopUpdateState(): DesktopUpdateState {
	return updateState;
}
