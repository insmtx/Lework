import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type FrontendEventExtra = Record<string, unknown>;

export type FrontendEvent = {
	event_type: string;
	timestamp: number;
	page_url?: string;
	page_title?: string;
	event_name?: string;
	duration_ms?: number;
	extra?: FrontendEventExtra;
	error_message?: string;
	error_stack?: string;
	component?: string;
};

export type CollectFrontendEventsParams = {
	fingerprint: string;
	events: FrontendEvent[];
};

export const FRONTEND_EVENT_ENDPOINT = "/CollectFrontendEvent";

export const frontendEventApi = {
	collect: (params: CollectFrontendEventsParams) =>
		apiClient.post<BackendDataResponse<null>>(FRONTEND_EVENT_ENDPOINT, params),
};
