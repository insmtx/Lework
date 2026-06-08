import { readStoredJwtToken } from "../utils/authStorage";
import { apiClient } from "./client";
import { API_BASE_URL } from "./config";
import type { BackendArtifact, BackendDataResponse } from "./types";

export function getArtifactDownloadUrl(artifactId: string): string {
	return `${API_BASE_URL}/artifacts/${encodeURIComponent(artifactId)}/download`;
}

export async function fetchArtifactDownload(
	artifactId: string,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	const token = readStoredJwtToken();
	const response = await fetch(getArtifactDownloadUrl(artifactId), {
		method: "GET",
		signal: options?.signal,
		headers: token ? { Authorization: `Bearer ${token}` } : undefined,
	});
	if (!response.ok) {
		throw new Error(`HTTP ${response.status}`);
	}
	return response;
}

export const artifactApi = {
	getDownloadUrl: getArtifactDownloadUrl,
	fetchDownload: fetchArtifactDownload,
	listTaskArtifacts: (taskId: string) =>
		apiClient.get<BackendDataResponse<BackendArtifact[]>>(
			`/tasks/${encodeURIComponent(taskId)}/artifacts`,
		),
};
