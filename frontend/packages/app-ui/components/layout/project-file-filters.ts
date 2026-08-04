import type { GetProjectFilesParams } from "@leros/store";

export type ProjectFileSourceFilter = "all" | "task" | "upload";

export type ProjectFileTypeFilter =
	| "all"
	| "folder"
	| "pdf"
	| "docx"
	| "xlsx"
	| "pptx"
	| "md"
	| "image"
	| "video"
	| "text";

export const PROJECT_FILE_TYPE_FILTER_OPTIONS: Array<{
	value: ProjectFileTypeFilter;
	label: string;
}> = [
	{ value: "all", label: "全部" },
	{ value: "folder", label: "文件夹" },
	{ value: "pdf", label: "PDF" },
	{ value: "docx", label: "Word" },
	{ value: "xlsx", label: "Excel" },
	{ value: "pptx", label: "PPT" },
	{ value: "md", label: "Markdown" },
	{ value: "image", label: "图片" },
	{ value: "video", label: "视频" },
	{ value: "text", label: "文本" },
];

export function isProjectFileFlatDisplay(typeFilter: ProjectFileTypeFilter): boolean {
	return typeFilter !== "all" && typeFilter !== "folder";
}

export function buildProjectFileListParams(
	projectId: string,
	sourceFilter: ProjectFileSourceFilter,
	typeFilter: ProjectFileTypeFilter,
): GetProjectFilesParams {
	const params: GetProjectFilesParams = { projectId };

	if (sourceFilter === "upload") {
		params.resourceType = "user_upload";
	} else if (sourceFilter === "task") {
		params.resourceType = "artifact";
	}

	if (typeFilter === "folder") {
		params.nodeType = "folder";
	} else if (typeFilter !== "all") {
		params.fileExt = typeFilter;
		params.nodeType = "file";
	}

	return params;
}
