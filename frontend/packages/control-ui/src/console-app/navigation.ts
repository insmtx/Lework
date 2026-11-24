import type { AppNavigation } from "@lework/app-ui";
import { automationPath, consoleRoutes, projectPath, taskPath, type ProjectTab } from "./routes";

/**
 * createConsoleNavigation 把宿主的路由能力收敛成 AppNavigation。
 *
 * 侧栏只负责发出「去哪个页面」，Web 传 Next 的 push、桌面传 react-router 的
 * navigate，页面路径统一来自 routes.ts，宿主之间不再各写一份映射。
 */
export function createConsoleNavigation(input: {
	pathname: string;
	push: (path: string) => void;
}): AppNavigation {
	const push = input.push;

	return {
		currentPath: input.pathname,
		supportsOpenCode: true,
		goToRoute(route) {
			push(viewPaths[route]);
		},
		goToPath(path) {
			push(path);
		},
		goToProject(projectId) {
			push(projectPath(projectId));
		},
		goToProjectTasks(projectId) {
			push(projectPath(projectId, "tasks"));
		},
		goToTaskDetail(projectId, taskId, sessionId) {
			push(taskPath(projectId, taskId, sessionId));
		},
		goToAutomationDetail(publicId) {
			push(automationPath(publicId));
		},
	};
}

// project 与 taskDetail 仍回落到 chat，保持 Web 现有行为：具体项目由侧栏
// 的 goToProject / goToTaskDetail 打开，goToRoute 只处理一级入口。
const viewPaths = {
	chat: consoleRoutes.chat,
	workbench: consoleRoutes.workbench,
	tasks: consoleRoutes.tasks,
	project: consoleRoutes.chat,
	projectsHub: consoleRoutes.projects,
	taskDetail: consoleRoutes.chat,
	orgProfile: consoleRoutes.orgProfile,
	orgDepartments: consoleRoutes.orgDepartments,
	orgAssistants: consoleRoutes.orgAssistants,
	orgModels: consoleRoutes.orgModels,
	skills: consoleRoutes.skills,
	automation: consoleRoutes.automation,
	settings: consoleRoutes.settings,
	workspaces: consoleRoutes.workspaces,
	nodes: consoleRoutes.nodes,
	pending: consoleRoutes.pending,
} satisfies Record<Parameters<AppNavigation["goToRoute"]>[0], string>;


export type { ProjectTab };
