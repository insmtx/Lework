import { apiClient } from "./client";
import type { BackendDataResponse, BackendNewMessageData } from "./types";

export type NewMessageParams = {
	content: string;
	execution_mode?: "default" | "plan";
	project_id?: string;
	task_id?: string;
	message_type?: string;
	assistant_id?: number;
	attachments?: {
		file_upload_id: string;
		name: string;
		mime_type: string;
		size?: number;
	}[];
};

const WORK_ENDPOINTS = {
	newMessage: "/NewMessage",
};

export const workApi = {
	newMessage: (params: NewMessageParams) =>
		apiClient.post<BackendDataResponse<BackendNewMessageData>>(WORK_ENDPOINTS.newMessage, params),
};
