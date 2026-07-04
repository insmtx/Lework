type PublicEnv = {
	readonly NEXT_PUBLIC_LEROS_API_BASE_URL?: string;
	readonly VITE_LEROS_API_BASE_URL?: string;
	readonly LEROS_API_BASE_URL?: string;
};

declare const process:
	| {
			readonly env?: PublicEnv;
	  }
	| undefined;

const DEFAULT_API_BASE_URL = "http://localhost:8080/v1";

function getNextAPIBaseURL(): string | undefined {
	if (typeof process === "undefined") return undefined;
	return process.env?.NEXT_PUBLIC_LEROS_API_BASE_URL || process.env?.LEROS_API_BASE_URL;
}

function getViteAPIBaseURL(): string | undefined {
	return (import.meta as ImportMeta & { readonly env?: PublicEnv }).env?.VITE_LEROS_API_BASE_URL;
}

function resolveAPIBaseURL(): string {
	const baseURL = getViteAPIBaseURL() || getNextAPIBaseURL() || DEFAULT_API_BASE_URL;

	return baseURL.trim().replace(/\/+$/, "");
}

export const API_BASE_URL = resolveAPIBaseURL();
