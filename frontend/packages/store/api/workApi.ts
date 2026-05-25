import { apiClient } from "./client";
import type { BackendBaseResponse } from "./types";

export type NewMessageParams = {
	content: string;
	project_id?: string;
	task_id?: string;
	message_type?: string;
	assistant_id?: number;
};

const WORK_ENDPOINTS = {
	newMessage: "/NewMessage",
};

export const workApi = {
	newMessage: (params: NewMessageParams) =>
		apiClient.post<BackendBaseResponse>(WORK_ENDPOINTS.newMessage, params),
};
