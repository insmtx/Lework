export const desktopUpdateEventChannel = "lework-desktop:update-event";
export const desktopUpdateGetStateChannel = "lework-desktop:update-get-state";
export const desktopUpdateCheckChannel = "lework-desktop:update-check";
export const desktopUpdateRestartChannel = "lework-desktop:update-restart";
export const desktopOpenPolicyPdfChannel = "lework-desktop:open-policy-pdf";
export const desktopOpenExternalChannel = "lework-desktop:open-external";

export type DesktopUpdatePhase =
	| "idle"
	| "checking"
	| "available"
	| "downloading"
	| "downloaded"
	| "up-to-date"
	| "error"
	| "unsupported";

export interface DesktopUpdateState {
	currentVersion: string;
	phase: DesktopUpdatePhase;
	message: string;
	availableVersion?: string;
	downloadedVersion?: string;
	progressPercent?: number;
	releaseDate?: string;
	releaseNotes?: string;
	lastCheckedAt?: string;
	canCheck: boolean;
	canRestart: boolean;
}

export type DesktopPolicyDocument = "terms" | "privacy";

export interface DesktopUpdateApi {
	getState: () => Promise<DesktopUpdateState>;
	checkForUpdates: () => Promise<DesktopUpdateState>;
	quitAndInstall: () => Promise<boolean>;
	openPolicyPdf: (document: DesktopPolicyDocument) => Promise<boolean>;
	openExternal: (url: string) => Promise<boolean>;
	subscribe: (listener: (state: DesktopUpdateState) => void) => () => void;
}
