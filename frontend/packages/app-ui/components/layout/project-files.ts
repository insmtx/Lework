import type { ProjectFileTypeFilter } from "./project-file-filters";

/** 项目文件表格列宽：名称列可伸缩，其余列固定宽度不压缩。 */
export const PROJECT_FILE_TABLE_GRID_CLASS =
	"grid grid-cols-[minmax(200px,1fr)_90px_90px_120px_180px_220px] items-center";

/** 表格最小宽度 = 名称列最小宽度 + 固定列宽之和 + 左右内边距。 */
export const PROJECT_FILE_TABLE_MIN_WIDTH_CLASS = "min-w-[960px]";

/** 表格行容器（不在行级加横向 padding，避免 sticky 列在滚动末端位移）。 */
export const PROJECT_FILE_TABLE_ROW_CLASS =
	"group/row border-b border-[var(--lework-control-border)]/60 py-4 transition-[background-color] hover:bg-[var(--lework-primary-softer)]/25";

/** 首列左侧留白，替代原 grid 上的 pl 方向 padding。 */
export const PROJECT_FILE_TABLE_LEADING_CELL_CLASS = "pl-5";

/** 横向滚动时固定在右侧的操作列表头。 */
export const PROJECT_FILE_TABLE_ACTIONS_HEADER_CLASS =
	"sticky right-0 z-30 shrink-0 border-l border-[var(--lework-control-border)]/60 bg-[var(--lework-surface-soft)] pl-3 pr-5 text-right shadow-[-6px_0_10px_-8px_rgba(15,23,42,0.14)]";

/** 横向滚动时固定在右侧的操作列单元格。 */
export const PROJECT_FILE_TABLE_ACTIONS_CELL_CLASS =
	"sticky right-0 z-20 flex shrink-0 items-center justify-end gap-1.5 border-l border-[var(--lework-control-border)]/60 bg-[var(--lework-surface)] pl-3 pr-5 shadow-[-6px_0_10px_-8px_rgba(15,23,42,0.14)] transition-[background-color] group-hover/row:bg-[var(--lework-surface-soft)]";

export type BackendProjectFileNodeLike = {
	name?: string;
	path?: string;
	relative_path?: string;
	type?: string;
	node_type?: string;
	parent_id?: string;
	parent_ids?: string[];
	children?: BackendProjectFileNodeLike[];
	size?: number;
	mime_type?: string;
	mod_time?: number;
	created_at?: number;
	public_id?: string;
	storage_uri?: string;
	initial_file_public_id?: string;
	version_no?: number;
	version_label?: string;
	version_count?: number;
	resource_type?: string;
};

export type ProjectFileNode = {
	name: string;
	path: string;
	type: "file" | "directory";
	nodeType: "file" | "folder";
	parentId: string;
	parentIds: string[];
	children: ProjectFileNode[];
	size: number;
	mimeType: string;
	modTime: number;
	createdAt: number;
	publicId: string;
	storageUri: string;
	initialFilePublicId: string;
	versionNo: number;
	versionLabel: string;
	versionCount: number;
	resourceType: string;
};

function resolveProjectFilePath(node: BackendProjectFileNodeLike): string {
	return normalizeFilePath(node.relative_path ?? node.path);
}

function normalizeFlatProjectFileNode(node: BackendProjectFileNodeLike): ProjectFileNode {
	const path = resolveProjectFilePath(node);
	const isDirectory =
		node.node_type === "folder" || node.type === "directory" || path.endsWith("/");
	const nodeType = isDirectory ? "folder" : "file";
	const pathSegments = path.replace(/\/+$/, "").split("/").filter(Boolean);
	const fallbackName = pathSegments[pathSegments.length - 1] ?? "";
	return {
		name: String(node.name ?? fallbackName),
		path,
		type: nodeType === "folder" ? "directory" : "file",
		nodeType,
		parentId: "",
		parentIds: [],
		size: typeof node.size === "number" ? node.size : 0,
		mimeType: typeof node.mime_type === "string" ? node.mime_type : "",
		modTime: typeof node.mod_time === "number" ? node.mod_time : 0,
		createdAt: typeof node.created_at === "number" ? node.created_at * 1000 : 0,
		publicId: typeof node.public_id === "string" ? node.public_id : "",
		storageUri: typeof node.storage_uri === "string" ? node.storage_uri : "",
		initialFilePublicId:
			typeof node.initial_file_public_id === "string" ? node.initial_file_public_id : "",
		versionNo: typeof node.version_no === "number" ? node.version_no : 0,
		versionLabel:
			typeof node.version_label === "string" && node.version_label
				? node.version_label
				: typeof node.version_no === "number" && node.version_no > 0
					? `第 ${node.version_no} 版`
					: "",
		versionCount: typeof node.version_count === "number" ? node.version_count : 0,
		resourceType: typeof node.resource_type === "string" ? node.resource_type : "",
		children: [],
	};
}

// 后端以平铺结构返回时，先归一化再按 relative_path 组装树。
export function parseProjectFileList(
	nodes: BackendProjectFileNodeLike[] | null | undefined,
): ProjectFileNode[] {
	if (!Array.isArray(nodes)) return [];
	const flat = flattenProjectFileList(nodes);
	return unwrapProjectFileStorageRoots(buildProjectFileTreeWithPaths(flat));
}

// 兼容仍返回嵌套 children 的旧响应。
export function normalizeProjectFileTree(
	nodes: BackendProjectFileNodeLike[] | null | undefined,
): ProjectFileNode[] {
	if (!Array.isArray(nodes)) return [];

	return nodes.map((node) => {
		const normalized = normalizeFlatProjectFileNode(node);
		normalized.children = normalizeProjectFileTree(node.children);
		return normalized;
	});
}

export function buildProjectFileTree(flatNodes: ProjectFileNode[]): ProjectFileNode[] {
	const byId = new Map<string, ProjectFileNode>();
	for (const node of flatNodes) {
		byId.set(node.publicId, { ...node, children: [] });
	}

	const roots: ProjectFileNode[] = [];
	for (const node of byId.values()) {
		if (node.parentId && byId.has(node.parentId)) {
			byId.get(node.parentId)?.children.push(node);
			continue;
		}
		roots.push(node);
	}

	sortProjectFileTreeNodes(roots);
	return roots;
}

function flattenProjectFileList(nodes: BackendProjectFileNodeLike[]): ProjectFileNode[] {
	const flat: ProjectFileNode[] = [];
	const seen = new Set<string>();

	const walk = (items: BackendProjectFileNodeLike[]) => {
		for (const item of items) {
			const normalized = normalizeFlatProjectFileNode(item);
			const dedupeKey = normalized.publicId || `${normalized.path}:${normalized.publicId}`;
			if (!seen.has(dedupeKey)) {
				seen.add(dedupeKey);
				flat.push(normalized);
			}
			if (Array.isArray(item.children) && item.children.length > 0) {
				walk(item.children);
			}
		}
	};

	walk(nodes);
	return flat;
}

function attachChildNode(
	parent: ProjectFileNode,
	child: ProjectFileNode,
	attachedAsChild: Set<ProjectFileNode>,
) {
	if (
		parent.children.some((item) => item.publicId === child.publicId && item.path === child.path)
	) {
		return;
	}
	parent.children.push(child);
	attachedAsChild.add(child);
}

export function buildProjectFileTreeWithPaths(flatNodes: ProjectFileNode[]): ProjectFileNode[] {
	const nodes = flatNodes.map((node) => ({ ...node, children: [] as ProjectFileNode[] }));
	const byPath = new Map<string, ProjectFileNode>();
	for (const node of nodes) {
		if (node.type === "directory") {
			byPath.set(normalizeDirPath(node.path), node);
		}
	}

	const attachedAsChild = new Set<ProjectFileNode>();

	for (const node of nodes) {
		const segments = normalizeFilePath(node.path).split("/").filter(Boolean);
		if (segments.length <= 1) {
			continue;
		}

		if (node.type === "directory") {
			const parentDir = ensureDirectoryChain(byPath, nodes, attachedAsChild, segments.slice(0, -1));
			attachChildNode(parentDir, node, attachedAsChild);
			attachedAsChild.add(node);
			continue;
		}

		const fileName = segments[segments.length - 1] ?? node.name;
		const parentDir = ensureDirectoryChain(byPath, nodes, attachedAsChild, segments.slice(0, -1));
		const fileNode: ProjectFileNode = { ...node, name: fileName, children: [] };
		attachChildNode(parentDir, fileNode, attachedAsChild);
		attachedAsChild.add(node);
	}

	const roots = nodes.filter((node) => !attachedAsChild.has(node));
	sortProjectFileTreeNodes(roots);
	return roots;
}

const PROJECT_FILE_STORAGE_ROOTS = new Set(["uploads", "artifacts"]);
const PROJECT_FILE_TASK_SCOPE_SEGMENT = "_task";

function isStorageRootNode(node: ProjectFileNode): boolean {
	if (node.type !== "directory") return false;
	const normalizedPath = normalizeFilePath(node.path).replace(/\/+$/, "");
	return (
		PROJECT_FILE_STORAGE_ROOTS.has(node.name) || PROJECT_FILE_STORAGE_ROOTS.has(normalizedPath)
	);
}

// 隐藏 uploads/、artifacts/ 以及 uploads/_task/{taskId}/ 这类内部路径，直接展示用户文件夹。
export function unwrapProjectFileStorageRoots(nodes: ProjectFileNode[]): ProjectFileNode[] {
	if (nodes.length === 0) return nodes;

	const storageRoots = nodes.filter((node) => isStorageRootNode(node));
	let result =
		storageRoots.length > 0
			? [
					...nodes.filter((node) => !isStorageRootNode(node)),
					...storageRoots.flatMap((node) => node.children),
				]
			: [...nodes];

	result = unwrapTaskScopeRoots(result);
	sortProjectFileTreeNodes(result);
	return result;
}

function unwrapTaskScopeRoots(nodes: ProjectFileNode[]): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];
	for (const node of nodes) {
		if (node.type === "directory" && node.name === PROJECT_FILE_TASK_SCOPE_SEGMENT) {
			for (const taskNode of node.children) {
				result.push(...unwrapTaskScopeRoots(taskNode.children));
			}
			continue;
		}
		result.push({
			...node,
			children: unwrapTaskScopeRoots(node.children),
		});
	}
	return result;
}

function ensureDirectoryChain(
	byPath: Map<string, ProjectFileNode>,
	nodes: ProjectFileNode[],
	attachedAsChild: Set<ProjectFileNode>,
	dirSegments: string[],
): ProjectFileNode {
	let currentPath = "";
	let currentNode: ProjectFileNode | null = null;

	for (const segment of dirSegments) {
		currentPath = currentPath ? `${currentPath}/${segment}` : segment;
		const dirPath = `${currentPath}/`;
		let dirNode = byPath.get(dirPath);
		if (!dirNode) {
			const virtualDir: ProjectFileNode = {
				name: segment,
				path: dirPath,
				type: "directory",
				nodeType: "folder",
				parentId: currentNode ? currentNode.publicId : "",
				parentIds: [],
				children: [],
				size: 0,
				mimeType: "",
				modTime: 0,
				createdAt: 0,
				publicId: `virtual:${dirPath}`,
				storageUri: "",
				initialFilePublicId: "",
				versionNo: 0,
				versionLabel: "",
				versionCount: 0,
				resourceType: "",
			};
			dirNode = virtualDir;
			byPath.set(dirPath, dirNode);
			nodes.push(dirNode);
		}

		if (currentNode && !currentNode.children.includes(dirNode)) {
			currentNode.children.push(dirNode);
			attachedAsChild.add(dirNode);
		}
		currentNode = dirNode;
	}

	if (!currentNode) {
		throw new Error("failed to resolve directory chain");
	}

	return currentNode;
}

function normalizeDirPath(path: string): string {
	const normalized = normalizeFilePath(path);
	if (!normalized) return "";
	const withoutTrailingSlash = normalized.replace(/\/+$/, "");
	if (PROJECT_FILE_STORAGE_ROOTS.has(withoutTrailingSlash)) {
		return `${withoutTrailingSlash}/`;
	}
	return normalized.endsWith("/") ? normalized : `${normalized}/`;
}

function sortProjectFileTreeNodes(nodes: ProjectFileNode[]) {
	nodes.sort(compareProjectFileNodes);
	for (const node of nodes) {
		if (node.children.length > 0) {
			sortProjectFileTreeNodes(node.children);
		}
	}
}

function compareProjectFileNodes(left: ProjectFileNode, right: ProjectFileNode) {
	const timeDiff = (right.createdAt ?? 0) - (left.createdAt ?? 0);
	if (timeDiff !== 0) {
		return timeDiff;
	}
	if (left.type !== right.type) {
		return left.type === "directory" ? -1 : 1;
	}
	return left.name.localeCompare(right.name, "zh-CN");
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

export function collectProjectFolderNodes(nodes: ProjectFileNode[]): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];

	const walk = (items: ProjectFileNode[]) => {
		for (const node of items) {
			if (node.type !== "directory") {
				continue;
			}
			const stats = getProjectFolderStats(node);
			result.push({
				...node,
				children: [],
				size: stats.size,
				createdAt: stats.createdAt || node.createdAt,
			});
			walk(node.children);
		}
	};

	walk(nodes);
	return result;
}

export function getProjectFileSearchSourceNodes(
	nodes: ProjectFileNode[],
	typeFilter: ProjectFileTypeFilter,
): ProjectFileNode[] {
	if (typeFilter === "folder") {
		return collectProjectFolderNodes(nodes);
	}
	if (typeFilter === "all") {
		return collectAllProjectFileNodes(nodes);
	}
	return collectSelectableFiles(nodes);
}

export function collectAllProjectFileNodes(nodes: ProjectFileNode[]): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];

	const walk = (items: ProjectFileNode[]) => {
		for (const node of items) {
			if (node.type === "directory") {
				const stats = getProjectFolderStats(node);
				result.push({
					...node,
					children: [],
					size: stats.size,
					createdAt: stats.createdAt || node.createdAt,
				});
				walk(node.children);
				continue;
			}
			result.push({ ...node, children: [] });
		}
	};

	walk(nodes);
	return result;
}

export function collectProjectFileNodes(nodes: ProjectFileNode[]): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];

	const walk = (items: ProjectFileNode[]) => {
		for (const node of items) {
			result.push({ ...node, children: [] });
			if (node.children.length > 0) {
				walk(node.children);
			}
		}
	};

	walk(nodes);
	return result;
}

export function getProjectFileFlatPathLabel(node: Pick<ProjectFileNode, "path">): string {
	return unwrapProjectFileDisplayPath(node.path).replace(/\//g, "\\");
}

export function matchesProjectFilePathSearch(node: ProjectFileNode, keyword: string): boolean {
	const normalizedKeyword = keyword.trim().toLowerCase();
	if (!normalizedKeyword) {
		return true;
	}
	return getProjectFileFullDisplayPath(node).toLowerCase().includes(normalizedKeyword);
}

export function filterProjectFileNodesBySearch(
	nodes: ProjectFileNode[],
	searchKeyword: string,
): ProjectFileNode[] {
	const keyword = searchKeyword.trim().toLowerCase();
	const allNodes = collectProjectFileNodes(nodes);
	if (!keyword) {
		return allNodes;
	}
	return allNodes.filter((node) => matchesProjectFilePathSearch(node, keyword));
}

export function findProjectFileNode(
	nodes: ProjectFileNode[],
	publicId: string,
	path: string,
): ProjectFileNode | undefined {
	for (const node of nodes) {
		if (publicId && node.publicId === publicId) {
			return node;
		}
		if (path && node.path === path) {
			return node;
		}
		const matched = findProjectFileNode(node.children, publicId, path);
		if (matched) {
			return matched;
		}
	}
	return undefined;
}

export function countProjectFiles(nodes: ProjectFileNode[]): number {
	return collectSelectableFiles(nodes).length;
}

export function getProjectFileLocationLabel(node: Pick<ProjectFileNode, "path">): string {
	const displayPath = unwrapProjectFileDisplayPath(node.path);
	const segments = displayPath.split("/").filter(Boolean);
	if (segments.length <= 1) {
		return "";
	}
	return segments.slice(0, -1).join("\\");
}

export function getProjectFileFullDisplayPath(
	node: Pick<ProjectFileNode, "path" | "type" | "name">,
): string {
	const displayPath = unwrapProjectFileDisplayPath(node.path).replace(/\/+$/, "");
	if (!displayPath) {
		return node.type === "directory" ? node.name : "";
	}
	return displayPath.replace(/\//g, "\\");
}

/** 扁平列表副标题：根目录单文件不展示路径，仅文件夹内文件展示完整相对路径。 */
export function getProjectFileFlatDisplayPathLabel(
	node: Pick<ProjectFileNode, "path" | "type" | "name">,
): string {
	const displayPath = unwrapProjectFileDisplayPath(node.path).replace(/\/+$/, "");
	const segments = displayPath.split("/").filter(Boolean);
	if (segments.length <= 1) {
		return "";
	}
	return displayPath.replace(/\//g, "\\");
}

export function filterProjectFileSearchResults(
	nodes: ProjectFileNode[],
	searchKeyword: string,
): ProjectFileNode[] {
	const keyword = searchKeyword.trim();
	if (!keyword) {
		return nodes;
	}
	return nodes.filter((node) => matchesProjectFilePathSearch(node, keyword));
}

// 将 uploads/、artifacts/、uploads/_task/{taskId}/ 等内部前缀转为用户可见路径。
export function unwrapProjectFileDisplayPath(path: string): string {
	const segments = unwrapProjectFileDisplaySegments(path);
	return segments.join("/");
}

function unwrapProjectFileDisplaySegments(path: string): string[] {
	let segments = normalizeFilePath(path).split("/").filter(Boolean);
	if (segments.length === 0) {
		return segments;
	}

	if (PROJECT_FILE_STORAGE_ROOTS.has(segments[0] ?? "")) {
		segments = segments.slice(1);
	}

	if (segments[0] === PROJECT_FILE_TASK_SCOPE_SEGMENT) {
		segments = segments.slice(2);
	}

	return segments;
}

export type FileSource = "task" | "upload";

export function getFileSource(path: string): FileSource {
	const normalized = normalizeFilePath(path);
	return normalized.startsWith("artifacts") ? "task" : "upload";
}

export function getProjectFileSourceLabel(
	node: Pick<ProjectFileNode, "path" | "resourceType">,
): string {
	if (node.resourceType === "artifact") {
		return "任务文件";
	}
	if (node.resourceType === "user_upload") {
		return "上传文件";
	}
	return getFileSource(node.path) === "task" ? "任务文件" : "上传文件";
}

export function getProjectFolderStats(node: ProjectFileNode): { size: number; createdAt: number } {
	let totalSize = 0;
	let latestChildCreatedAt = 0;

	const walk = (current: ProjectFileNode) => {
		if (current.type === "file") {
			totalSize += current.size;
			if (current.createdAt > latestChildCreatedAt) {
				latestChildCreatedAt = current.createdAt;
			}
			return;
		}
		for (const child of current.children) {
			walk(child);
		}
	};

	for (const child of node.children) {
		walk(child);
	}

	return {
		size: totalSize,
		createdAt: node.createdAt || latestChildCreatedAt,
	};
}

export function getProjectFileTypeLabel(
	file: Pick<ProjectFileNode, "name" | "mimeType" | "type" | "nodeType">,
): string {
	if (file.type === "directory" || file.nodeType === "folder") {
		return "文件夹";
	}

	const extension = file.name.match(/\.([^.]+)$/)?.[1];
	if (extension) return extension.toUpperCase();

	const mimeSubtype = file.mimeType.split("/")[1]?.split(";")[0];
	return mimeSubtype ? mimeSubtype.toUpperCase() : "未知";
}

export function sortProjectFilesByUploadedTimeDesc(files: ProjectFileNode[]): ProjectFileNode[] {
	return [...files].sort((left, right) => right.createdAt - left.createdAt);
}

export function filterProjectFileTree(
	nodes: ProjectFileNode[],
	predicate: (node: ProjectFileNode) => boolean,
): ProjectFileNode[] {
	const result: ProjectFileNode[] = [];
	for (const node of nodes) {
		const filteredChildren = filterProjectFileTree(node.children, predicate);
		if (predicate(node) || filteredChildren.length > 0) {
			result.push({
				...node,
				children: filteredChildren,
			});
		}
	}
	return result;
}

function normalizeFilePath(path: string | undefined): string {
	if (!path) return "";
	return path.replace(/^\/+/, "");
}
