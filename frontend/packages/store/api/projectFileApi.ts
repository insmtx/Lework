import {
	COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE,
	COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE,
	resolveComposerUploadFileName,
} from "../constants/composer-upload";
import { authenticatedFetch } from "../utils/authStorage";
import { apiClient } from "./client";
import { API_BASE_URL } from "./config";
import type {
	BackendDataResponse,
	BackendProjectFileNode,
	BackendProjectFileUploadResult,
	BackendProjectFileVersionList,
} from "./types";

export type GetProjectFilesParams = {
	projectId: string;
	resourceType?: "user_upload" | "artifact";
	taskId?: string;
	nodeType?: "folder" | "file";
	fileExt?: string;
};

export type UploadProjectFileParams = {
	projectId: string;
	projectPublicId: string;
	file: File;
	signal?: AbortSignal;
};

export type UploadLooseFileParams = {
	file: File;
	purpose?: string;
	source_id?: string;
	/** 对话输入框上传时传 true，后端会按 local-path 后缀校验 */
	withLocalPath?: boolean;
	signal?: AbortSignal;
};

type BackendUploadFilePayload = {
	public_id: string;
	filename?: string;
	original_name?: string;
	mime_type?: string;
	file_size?: number;
	sha256?: string;
	storage_uri?: string;
};

async function parseErrorMessage(response: Response): Promise<string> {
	let message = `HTTP ${response.status}`;
	try {
		const payload = (await response.json()) as { message?: string };
		if (typeof payload.message === "string" && payload.message) {
			message = payload.message;
		}
	} catch {
		// 保持默认错误信息即可
	}
	if (message === "unsupported file type") {
		return COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE;
	}
	if (message === "empty file is not allowed") {
		return COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE;
	}
	return message;
}

const listProjectFilesInflight = new Map<
	string,
	ReturnType<typeof apiClient.get<BackendDataResponse<BackendProjectFileNode[]>>>
>();

function getListProjectFilesKey(params: GetProjectFilesParams): string {
	return [
		params.projectId,
		params.resourceType ?? "",
		params.taskId ?? "",
		params.nodeType ?? "",
		params.fileExt ?? "",
	].join(":");
}

function assertBackendSuccess<T>(
	response: BackendDataResponse<T>,
	fallbackMessage: string,
): BackendDataResponse<T> {
	if (response.code !== 0) {
		throw new Error(response.message || fallbackMessage);
	}
	return response;
}

async function uploadFile(
	file: File,
	projectPublicId: string,
	signal?: AbortSignal,
): Promise<BackendDataResponse<BackendUploadFilePayload>> {
	return uploadLooseFile({
		file,
		purpose: "projects",
		source_id: projectPublicId,
		withLocalPath: true,
		signal,
	});
}

async function uploadLooseFile({
	file,
	purpose = "attachment",
	source_id,
	withLocalPath = false,
	signal,
}: UploadLooseFileParams): Promise<BackendDataResponse<BackendUploadFilePayload>> {
	const formData = new FormData();
	formData.append("file", file);
	formData.append("purpose", purpose);
	if (source_id) {
		formData.append("source_id", source_id);
	}
	if (withLocalPath) {
		formData.append("local-path", resolveComposerUploadFileName(file));
	}

	const response = await authenticatedFetch(`${API_BASE_URL}/files/upload`, {
		method: "POST",
		body: formData,
		signal,
	});

	if (!response.ok) {
		throw new Error(await parseErrorMessage(response));
	}

	return (await response.json()) as BackendDataResponse<BackendUploadFilePayload>;
}

export const projectFileApi = {
	list: (params: GetProjectFilesParams) => {
		const key = getListProjectFilesKey(params);
		const inflight = listProjectFilesInflight.get(key);
		if (inflight) return inflight;

		const queryParams: Record<string, string> = {};
		if (params.resourceType) queryParams.resource_type = params.resourceType;
		if (params.taskId) queryParams.task_id = params.taskId;
		if (params.nodeType) queryParams.node_type = params.nodeType;
		if (params.fileExt) queryParams.file_ext = params.fileExt;

		const promise = apiClient
			.get<BackendDataResponse<BackendProjectFileNode[]>>(
				`/projects/${encodeURIComponent(params.projectId)}/files`,
				{
					params: Object.keys(queryParams).length > 0 ? queryParams : undefined,
				},
			)
			.finally(() => {
				listProjectFilesInflight.delete(key);
			});
		listProjectFilesInflight.set(key, promise);
		return promise;
	},

	download: (projectId: string, filePath: string): string =>
		`${API_BASE_URL}/projects/${encodeURIComponent(projectId)}/files/download?path=${encodeURIComponent(filePath)}`,

	downloadVersion: (projectId: string, filePublicId: string): string =>
		`${API_BASE_URL}/projects/${encodeURIComponent(projectId)}/files/${encodeURIComponent(filePublicId)}/download`,

	folderDownload: (projectId: string, folderPublicId: string): string =>
		`${API_BASE_URL}/projects/${encodeURIComponent(projectId)}/files/folders/${encodeURIComponent(folderPublicId)}/download`,

	async fetchFolderDownload(
		projectId: string,
		folderPublicId: string,
		options?: { signal?: AbortSignal },
	): Promise<Response> {
		const url = projectFileApi.folderDownload(projectId, folderPublicId);
		const response = await authenticatedFetch(url, {
			method: "GET",
			signal: options?.signal,
		});
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}`);
		}
		return response;
	},

	async fetchDownload(
		projectId: string,
		filePath: string,
		options?: { signal?: AbortSignal },
	): Promise<Response> {
		const url = projectFileApi.download(projectId, filePath);
		const response = await authenticatedFetch(url, {
			method: "GET",
			signal: options?.signal,
		});
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}`);
		}
		return response;
	},

	async fetchDownloadVersion(
		projectId: string,
		filePublicId: string,
		options?: { signal?: AbortSignal },
	): Promise<Response> {
		const response = await authenticatedFetch(
			projectFileApi.downloadVersion(projectId, filePublicId),
			{
				method: "GET",
				signal: options?.signal,
			},
		);
		if (!response.ok) {
			throw new Error(`HTTP ${response.status}`);
		}
		return response;
	},

	versions: (projectId: string, filePublicId: string) =>
		apiClient.get<BackendDataResponse<BackendProjectFileVersionList>>(
			`/projects/${encodeURIComponent(projectId)}/files/${encodeURIComponent(filePublicId)}/versions`,
		),

	restoreVersion: (projectId: string, filePublicId: string) =>
		apiClient.post<BackendDataResponse<BackendProjectFileNode>>(
			`/projects/${encodeURIComponent(projectId)}/files/${encodeURIComponent(filePublicId)}/restore`,
		),

	upload: async ({ file, projectPublicId, signal }: UploadProjectFileParams) => {
		const uploadResponse = assertBackendSuccess(
			await uploadFile(file, projectPublicId, signal),
			"文件上传失败",
		);
		const uploaded = uploadResponse.data;
		if (!uploaded?.public_id) {
			throw new Error("上传接口未返回 public_id");
		}

		return {
			code: uploadResponse.code,
			message: uploadResponse.message,
			data: {
				path: uploaded.storage_uri || uploaded.public_id,
				filename: uploaded.original_name || uploaded.filename || file.name,
				size: uploaded.file_size ?? file.size,
				public_id: uploaded.public_id,
				original_name: uploaded.original_name,
				mime_type: uploaded.mime_type || file.type,
				file_size: uploaded.file_size ?? file.size,
				sha256: uploaded.sha256,
				storage_uri: uploaded.storage_uri,
			} satisfies BackendProjectFileUploadResult,
		} as BackendDataResponse<BackendProjectFileUploadResult>;
	},

	uploadLoose: async ({
		file,
		purpose = "attachment",
		withLocalPath,
		signal,
	}: UploadLooseFileParams) => {
		const uploadResponse = assertBackendSuccess(
			await uploadLooseFile({
				file,
				purpose,
				withLocalPath,
				signal,
			}),
			"文件上传失败",
		);
		const uploaded = uploadResponse.data;
		if (!uploaded?.public_id) {
			throw new Error("上传接口未返回 public_id");
		}

		return {
			code: uploadResponse.code,
			message: uploadResponse.message,
			data: {
				path: uploaded.storage_uri || uploaded.public_id,
				filename: uploaded.original_name || uploaded.filename || file.name,
				size: uploaded.file_size ?? file.size,
				public_id: uploaded.public_id,
				original_name: uploaded.original_name,
				mime_type: uploaded.mime_type || file.type,
				file_size: uploaded.file_size ?? file.size,
				sha256: uploaded.sha256,
				storage_uri: uploaded.storage_uri,
			} satisfies BackendProjectFileUploadResult,
		} as BackendDataResponse<BackendProjectFileUploadResult>;
	},
};
