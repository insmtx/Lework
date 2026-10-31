export const CLIENT_UPGRADE_REQUIRED_EVENT = "lework:client-upgrade-required";

export type ClientApp = "desktop" | "web";

export type ClientVersionReportParams = {
	app: ClientApp;
	version: string;
	platform?: string;
	arch?: string;
	channel?: string;
};

export type ClientUpdatePolicy = {
	force_update: boolean;
	app?: string;
	current_version?: string;
	min_supported_version?: string;
	latest_version?: string;
	update_url?: string;
	message?: string;
};

export type ClientUpgradeRequiredEvent = CustomEvent<ClientUpdatePolicy>;

type PublicEnv = {
	readonly VITE_LEWORK_APP_VERSION?: string;
	readonly NEXT_PUBLIC_LEWORK_APP_VERSION?: string;
};

declare const process:
	| {
			readonly env?: PublicEnv;
	  }
	| undefined;

function getViteAppVersion(): string | undefined {
	return (import.meta as ImportMeta & { readonly env?: PublicEnv }).env?.VITE_LEWORK_APP_VERSION;
}

function getNextAppVersion(): string | undefined {
	if (typeof process === "undefined") return undefined;
	return process.env?.NEXT_PUBLIC_LEWORK_APP_VERSION;
}

export function getClientVersionReport(): ClientVersionReportParams {
	return {
		app: getClientApp(),
		version: getClientVersion(),
		platform: getClientPlatform(),
		arch: getClientArch(),
		channel: "stable",
	};
}

export function getClientHeaders(): Record<string, string> {
	const report = getClientVersionReport();
	return {
		"X-Lework-Client-App": report.app,
		"X-Lework-Client-Version": report.version,
		"X-Lework-Client-Platform": report.platform || "unknown",
		"X-Lework-Client-Arch": report.arch || "unknown",
	};
}

export function dispatchClientUpgradeRequired(policy: ClientUpdatePolicy) {
	if (typeof window === "undefined") return;
	window.dispatchEvent(new CustomEvent(CLIENT_UPGRADE_REQUIRED_EVENT, { detail: policy }));
}

export function readClientUpgradePolicy(value: unknown): ClientUpdatePolicy | null {
	if (!isRecord(value) || !isRecord(value.data)) {
		return null;
	}

	const data = value.data;
	if (data.force_update !== true) {
		return null;
	}

	return {
		force_update: true,
		app: stringValue(data.app),
		current_version: stringValue(data.current_version),
		min_supported_version: stringValue(data.min_supported_version),
		latest_version: stringValue(data.latest_version),
		update_url: stringValue(data.update_url),
		message: stringValue(data.message) || stringValue(value.message),
	};
}

function getClientApp(): ClientApp {
	if (typeof window !== "undefined" && "leworkDesktop" in window) {
		return "desktop";
	}
	return "web";
}

function getClientVersion(): string {
	return getViteAppVersion() || getNextAppVersion() || "0.0.0";
}

function getClientPlatform(): string {
	if (typeof navigator === "undefined") {
		return "unknown";
	}

	return navigator.platform || "unknown";
}

function getClientArch(): string {
	if (typeof navigator === "undefined") {
		return "unknown";
	}

	const userAgent = navigator.userAgent.toLowerCase();
	if (userAgent.includes("arm64") || userAgent.includes("aarch64")) {
		return "arm64";
	}
	if (userAgent.includes("x86_64") || userAgent.includes("wow64") || userAgent.includes("win64")) {
		return "x64";
	}
	return "unknown";
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function stringValue(value: unknown): string | undefined {
	if (typeof value !== "string") return undefined;
	const trimmed = value.trim();
	return trimmed || undefined;
}
