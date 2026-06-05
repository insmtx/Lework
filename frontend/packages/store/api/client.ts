import type { HttpClient } from "@leros/ui/lib/request";
import { createHttpClient } from "@leros/ui/lib/request";
import { readStoredJwtToken } from "../utils/authStorage";
import { API_BASE_URL } from "./config";

export const apiClient: HttpClient = createHttpClient(API_BASE_URL);

// biome-ignore lint/correctness/useHookAtTopLevel: this is an HTTP client interceptor API, not a React hook.
apiClient.useRequestInterceptor((config) => {
	const token = readStoredJwtToken();
	if (!token) return config;

	return {
		...config,
		headers: {
			...headersToRecord(config.headers),
			Authorization: `Bearer ${token}`,
		},
	};
});

function headersToRecord(headers: HeadersInit | undefined): Record<string, string> {
	if (!headers) return {};
	if (headers instanceof Headers) return Object.fromEntries(headers.entries());
	if (Array.isArray(headers)) return Object.fromEntries(headers);
	return headers;
}
