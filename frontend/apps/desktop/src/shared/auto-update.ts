export const desktopUpdateEventChannel = "leros-desktop:update-event";
export const desktopUpdateGetStateChannel = "leros-desktop:update-get-state";
export const desktopUpdateCheckChannel = "leros-desktop:update-check";
export const desktopUpdateRestartChannel = "leros-desktop:update-restart";
export const desktopOpenPolicyPdfChannel = "leros-desktop:open-policy-pdf";

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
	subscribe: (listener: (state: DesktopUpdateState) => void) => () => void;
}
