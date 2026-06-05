class HttpClient {
	private baseURL: string;
	private defaultHeaders: Record<string, string>;
	private interceptors: {
		request: Array<(config: RequestInit) => RequestInit>;
		response: Array<
			(response: globalThis.Response) => globalThis.Response | Promise<globalThis.Response>
		>;
	};

	constructor(baseURL = "", defaultHeaders: Record<string, string> = {}) {
		this.baseURL = baseURL;
		this.defaultHeaders = {
			"Content-Type": "application/json",
			...defaultHeaders,
		};
		this.interceptors = {
			request: [],
			response: [],
		};
	}

	useRequestInterceptor(interceptor: (config: RequestInit) => RequestInit): () => void {
		this.interceptors.request.push(interceptor);
		return () => {
			const index = this.interceptors.request.indexOf(interceptor);
			if (index > -1) this.interceptors.request.splice(index, 1);
		};
	}

	useResponseInterceptor(
		interceptor: (
			response: globalThis.Response,
		) => globalThis.Response | Promise<globalThis.Response>,
	): () => void {
		this.interceptors.response.push(interceptor);
		return () => {
			const index = this.interceptors.response.indexOf(interceptor);
			if (index > -1) this.interceptors.response.splice(index, 1);
		};
	}

	private buildURL(url: string, params?: Record<string, string | number | boolean>): string {
		const fullURL = this.baseURL ? `${this.baseURL}${url}` : url;
		if (!params) return fullURL;

		const urlObj = new URL(fullURL, window.location.origin);
		Object.keys(params).forEach((key) => {
			urlObj.searchParams.append(key, String(params[key]));
		});
		return urlObj.toString();
	}

	private async requestWithTimeout(
		url: string,
		options: RequestInit,
		timeout: number,
	): Promise<globalThis.Response> {
		const controller = new AbortController();
		const timeoutId = setTimeout(() => controller.abort(), timeout);

		try {
			const response = await fetch(url, {
				...options,
				signal: controller.signal,
			});
			return response;
		} finally {
			clearTimeout(timeoutId);
		}
	}

	async request<T>(url: string, options: RequestOptions = {}): Promise<ApiResponse<T>> {
		const {
			timeout = 30000,
			retryCount = 0,
			retryDelay = 1000,
			params,
			headers,
			...fetchOptions
		} = options;

		let config: RequestInit = {
			...fetchOptions,
			headers: {
				...this.defaultHeaders,
				...headers,
			},
		};

		for (const interceptor of this.interceptors.request) {
			config = interceptor(config);
		}

		const fullURL = this.buildURL(url, params);

		let lastError: ApiError | null = null;
		let attempts = 0;

		while (attempts <= retryCount) {
			try {
				let response = await this.requestWithTimeout(fullURL, config, timeout);

				for (const interceptor of this.interceptors.response) {
					response = await interceptor(response);
				}

				if (!response.ok) {
					const error: ApiError = new Error(`HTTP Error: ${response.status}`);
					error.status = response.status;
					error.statusText = response.statusText;
					const errorData = await readResponseJSON(response);
					if (errorData !== undefined) {
						error.response = {
							data: errorData,
							status: response.status,
							statusText: response.statusText,
							headers: response.headers,
						};
						if (isErrorResponse(errorData)) {
							error.message = errorData.message;
						}
					}
					throw error;
				}

				const data = await response.json();

				return {
					data,
					status: response.status,
					statusText: response.statusText,
					headers: response.headers,
				};
			} catch (error) {
				lastError = error as ApiError;

				if (lastError?.status && lastError.status < 500) {
					throw lastError;
				}

				attempts++;
				if (attempts <= retryCount) {
					await new Promise((resolve) => setTimeout(resolve, retryDelay * attempts));
				}
			}
		}

		throw lastError;
	}

	get<T>(url: string, options?: RequestOptions): Promise<ApiResponse<T>> {
		return this.request<T>(url, { ...options, method: "GET" });
	}

	post<T>(url: string, body?: unknown, options?: RequestOptions): Promise<ApiResponse<T>> {
		return this.request<T>(url, {
			...options,
			method: "POST",
			body: body ? JSON.stringify(body) : undefined,
		});
	}

	put<T>(url: string, body?: unknown, options?: RequestOptions): Promise<ApiResponse<T>> {
		return this.request<T>(url, {
			...options,
			method: "PUT",
			body: body ? JSON.stringify(body) : undefined,
		});
	}

	patch<T>(url: string, body?: unknown, options?: RequestOptions): Promise<ApiResponse<T>> {
		return this.request<T>(url, {
			...options,
			method: "PATCH",
			body: body ? JSON.stringify(body) : undefined,
		});
	}

	delete<T>(url: string, options?: RequestOptions): Promise<ApiResponse<T>> {
		return this.request<T>(url, { ...options, method: "DELETE" });
	}
}

export const http = new HttpClient();

export function createHttpClient(
	baseURL?: string,
	defaultHeaders?: Record<string, string>,
): HttpClient {
	return new HttpClient(baseURL, defaultHeaders);
}

export type { HttpClient };

export interface RequestOptions extends RequestInit {
	timeout?: number;
	retryCount?: number;
	retryDelay?: number;
	baseURL?: string;
	params?: Record<string, string | number | boolean>;
}

export interface ApiResponse<T = unknown> {
	data: T;
	status: number;
	statusText: string;
	headers: Headers;
}

export interface ApiError extends Error {
	status?: number;
	statusText?: string;
	response?: ApiResponse;
}

async function readResponseJSON(response: globalThis.Response): Promise<unknown> {
	try {
		return await response.clone().json();
	} catch {
		return undefined;
	}
}

function isErrorResponse(value: unknown): value is { message: string } {
	return (
		typeof value === "object" &&
		value !== null &&
		"message" in value &&
		typeof (value as { message?: unknown }).message === "string"
	);
}
