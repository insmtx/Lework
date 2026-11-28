/**
 * 控制台路由的唯一事实来源。
 *
 * Web 的文件路由与桌面的 react-router 声明都从这里取路径，避免两份路由
 * 各自漂移；动态段统一走下面的构造函数，保证 URL 编码一致。
 */
export const consoleRoutes = {
	chat: "/chat",
	workbench: "/workbench",
	payroll: "/workbench/payroll",
	projects: "/projects",
	orgProfile: "/org/profile",
	orgDepartments: "/org/departments",
	orgAssistants: "/org/assistants",
	orgModels: "/org/models",
	skills: "/skills",
	automation: "/automation",
	tasks: "/tasks",
	workspaces: "/workspaces",
	workspacesConsole: "/workspaces/console",
	nodes: "/nodes",
	pending: "/pending",
	settings: "/settings",
	knowledge: "/knowledge",
} as const;

export type ConsoleRouteKey = keyof typeof consoleRoutes;

export type ProjectTab = "chat" | "tasks" | "files" | "activity";

export function projectPath(projectId: string, tab: ProjectTab = "chat"): string {
	const base = `/projects/${encodeURIComponent(projectId)}`;
	if (tab === "chat") return base;
	if (tab === "tasks") return `${base}/tasks`;
	if (tab === "files") return `${base}/files`;
	return `${base}/activity`;
}

export function taskPath(projectId: string, taskId: string, sessionId: string): string {
	return `${projectPath(projectId, "tasks")}/${encodeURIComponent(taskId)}/sessions/${encodeURIComponent(sessionId)}`;
}

export function skillPath(skillId: string): string {
	return `${consoleRoutes.skills}/${encodeURIComponent(skillId)}`;
}

export function automationPath(publicId: string): string {
	return `${consoleRoutes.automation}/${encodeURIComponent(publicId)}`;
}

export function workspacePath(key: string): string {
	return `${consoleRoutes.workspaces}/${encodeURIComponent(key)}`;
}

export function workspaceFilesPath(key: string): string {
	return `${workspacePath(key)}/files`;
}

export function consoleSessionPath(nodeID: string, sessionID: string): string {
	return `${consoleRoutes.workspacesConsole}?node=${encodeURIComponent(nodeID)}&session=${encodeURIComponent(sessionID)}`;
}
