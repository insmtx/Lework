import { fetchFilePreviewByStorageUri, projectFileApi } from "@lework/store";
import JSZip from "jszip";
import {
	collectSelectableFiles,
	findProjectFileNode,
	type ProjectFileNode,
	unwrapProjectFileDisplayPath,
} from "./project-files";

function normalizeStoragePath(path: string): string {
	return path.replace(/^\/+/, "").replace(/\/+$/, "");
}

export function resolveProjectFolderNode(
	folder: ProjectFileNode,
	fullTree?: ProjectFileNode[],
): ProjectFileNode {
	if (folder.type !== "directory") {
		return folder;
	}
	if (folder.children.length > 0) {
		return folder;
	}
	if (!fullTree?.length) {
		return folder;
	}
	return findProjectFileNode(fullTree, folder.publicId, folder.path) ?? folder;
}

export function getFolderZipEntryPath(folder: ProjectFileNode, file: ProjectFileNode): string {
	const folderPrefix = normalizeStoragePath(folder.path);
	const filePath = normalizeStoragePath(file.path);

	let innerPath: string;
	if (folderPrefix && filePath.startsWith(`${folderPrefix}/`)) {
		innerPath = filePath.slice(folderPrefix.length + 1);
	} else {
		const folderDisplay = normalizeStoragePath(unwrapProjectFileDisplayPath(folder.path));
		const fileDisplay = normalizeStoragePath(unwrapProjectFileDisplayPath(file.path));
		if (folderDisplay && fileDisplay.startsWith(`${folderDisplay}/`)) {
			innerPath = fileDisplay.slice(folderDisplay.length + 1);
		} else {
			innerPath = file.name;
		}
	}

	const rootName = folder.name.trim() || "folder";
	return `${rootName}/${innerPath}`.replace(/\\/g, "/");
}

async function fetchProjectFileBlob(projectId: string, file: ProjectFileNode): Promise<Blob> {
	const isVirtualId = file.publicId.startsWith("virtual:");
	let response: Response;

	if (file.storageUri) {
		response = await fetchFilePreviewByStorageUri(file.storageUri);
	} else if (file.publicId && !isVirtualId) {
		response = await projectFileApi.fetchDownloadVersion(projectId, file.publicId);
	} else {
		response = await projectFileApi.fetchDownload(projectId, file.path);
	}

	if (!response.ok) {
		throw new Error(`下载失败：${file.name} (HTTP ${response.status})`);
	}

	return response.blob();
}

export async function downloadProjectFolderAsZip(
	projectId: string,
	folder: ProjectFileNode,
	fullTree?: ProjectFileNode[],
): Promise<Blob> {
	const resolvedFolder = resolveProjectFolderNode(folder, fullTree);
	const filesToZip = collectSelectableFiles([resolvedFolder]);
	if (filesToZip.length === 0) {
		throw new Error("文件夹为空，无法下载");
	}

	const zip = new JSZip();
	for (const file of filesToZip) {
		const blob = await fetchProjectFileBlob(projectId, file);
		zip.file(getFolderZipEntryPath(resolvedFolder, file), blob);
	}

	return zip.generateAsync({ type: "blob" });
}

export function triggerBlobDownload(blob: Blob, filename: string) {
	const objectUrl = URL.createObjectURL(blob);
	const link = document.createElement("a");
	link.href = objectUrl;
	link.download = filename;
	document.body.appendChild(link);
	link.click();
	link.remove();
	window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
}
