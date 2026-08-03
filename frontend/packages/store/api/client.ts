import type { HttpClient } from "@leros/ui/lib/request";
import { createHttpClient } from "@leros/ui/lib/request";
import { getValidJwtToken, readStoredAuthUser } from "../utils/authStorage";
import {
	dispatchClientUpgradeRequired,
	getClientHeaders,
	readClientUpgradePolicy,
} from "./clientUpdatePolicy";
import { API_BASE_URL } from "./config";

export const apiClient: HttpClient = createHttpClient(API_BASE_URL);

// biome-ignore lint/correctness/useHookAtTopLevel: this is an HTTP client interceptor API, not a React hook.
apiClient.useRequestInterceptor(async (config) => {
	const token = await getValidJwtToken();
	const nextConfig = withClientHeaders(config);
	if (!token) return nextConfig;

	return withAuthorization(nextConfig, token);
});

// biome-ignore lint/correctness/useHookAtTopLevel: this is an HTTP client interceptor API, not a React hook.
apiClient.useResponseInterceptor(async (response, context) => {
	if (response.status === 426) {
		const payload = await readResponseJSON(response);
		const policy = readClientUpgradePolicy(payload);
		if (policy) {
			dispatchClientUpgradeRequired(policy);
		}
		return response;
	}

	if (response.status !== 401 || isAuthEndpoint(context.url) || !readStoredAuthUser()) {
		return response;
	}

	const token = await getValidJwtToken(true);
	if (!token) return response;
	return fetch(context.url, withAuthorization(context.config, token));
});

function headersToRecord(headers: HeadersInit | undefined): Record<string, string> {
	if (!headers) return {};
	if (headers instanceof Headers) return Object.fromEntries(headers.entries());
	if (Array.isArray(headers)) return Object.fromEntries(headers);
	return headers;
}

function withAuthorization(config: RequestInit, token: string): RequestInit {
	return {
		...config,
		headers: {
			...headersToRecord(config.headers),
			Authorization: `Bearer ${token}`,
		},
	};
}

function withClientHeaders(config: RequestInit): RequestInit {
	return {
		...config,
		headers: {
			...headersToRecord(config.headers),
			...getClientHeaders(),
		},
	};
}

async function readResponseJSON(response: Response): Promise<unknown> {
	try {
		return await response.clone().json();
	} catch {
		return undefined;
	}
}

function isAuthEndpoint(url: string): boolean {
	return [
		"/LoginByPassword",
		"/RegisterByEmail",
		"/SendPhoneLoginCode",
		"/LoginByPhoneCode",
		"/RefreshToken",
	].some((path) => url.endsWith(path));
}
