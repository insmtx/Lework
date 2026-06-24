export type BackendProjectFileNodeLike = {
	name?: string;
	path?: string;
	type?: string;
	children?: BackendProjectFileNodeLike[];
	size?: number;
	mime_type?: string;
	mod_time?: number;
	created_at?: number;
	public_id?: string;
};

export type ProjectFileNode = {
	name: string;
	path: string;
	type: "file" | "directory";
	children: ProjectFileNode[];
	size: number;
	mimeType: string;
	modTime: number;
	createdAt: number;
	publicId: string;
};

// 统一清洗后端文件树结构，避免页面层到处处理空值和字段名差异。
export function normalizeProjectFileTree(
	nodes: BackendProjectFileNodeLike[] | null | undefined,
): ProjectFileNode[] {
	if (!Array.isArray(nodes)) return [];

	return nodes.map((node) => ({
		name: String(node.name ?? ""),
		path: normalizeFilePath(node.path),
		type: node.type === "directory" ? "directory" : "file",
		size: typeof node.size === "number" ? node.size : 0,
		mimeType: typeof node.mime_type === "string" ? node.mime_type : "",
		modTime: typeof node.mod_time === "number" ? node.mod_time : 0,
		createdAt: typeof node.created_at === "number" ? node.created_at : 0,
		publicId: typeof node.public_id === "string" ? node.public_id : "",
		children: normalizeProjectFileTree(node.children),
	}));
}

// 文件页默认要预览第一个文件，所以这里直接给出可选文件的稳定顺序。
export function collectSelectableFiles(nodes: ProjectFileNode[]): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];

	for (const node of nodes) {
		if (node.type === "file") {
			result.push(node);
			continue;
		}
		result.push(...collectSelectableFiles(node.children));
	}

	return result;
}

export type FileSource = "task" | "upload";

export function getFileSource(path: string): FileSource {
	const normalized = normalizeFilePath(path);
	return normalized.startsWith("artifacts") ? "task" : "upload";
}

export function sortProjectFilesByUploadedTimeDesc(files: ProjectFileNode[]): ProjectFileNode[] {
	return [...files].sort((left, right) => right.createdAt - left.createdAt);
}

function normalizeFilePath(path: string | undefined): string {
	if (!path) return "";
	return path.replace(/^\/+/, "");
}
