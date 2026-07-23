import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type Edition = "oss" | "enterprise";

export type GlobalConfig = {
	edition: Edition;
};

export const globalConfigApi = {
	get: () => apiClient.get<BackendDataResponse<GlobalConfig>>("/GlobalConfig"),
};
