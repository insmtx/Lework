"use client";

import { type AppNavigation, Shell } from "@leros/app-ui";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";

export function LerosShell({ children }: { children: ReactNode }) {
	const navigation = useWebNavigation();

	return <Shell navigation={navigation}>{children}</Shell>;
}

export function useWebNavigation(): AppNavigation {
	const pathname = usePathname();
	const router = useRouter();

	return {
		currentPath: pathname,
		goToRoute(route) {
			const routePath = {
				workbench: "/workbench",
				tasks: "/tasks",
				project: "/workbench",
				projectsHub: "/projects",
				taskDetail: "/workbench",
				digitalAssistant: "/assistants",
				aiTeammates: "/assistants",
				knowledge: "/knowledge",
				skills: "/skills",
				settings: "/settings",
			}[route];
			if (!routePath) {
				router.push("/workbench");
				return;
			}
			router.push(routePath);
		},
		goToProject(projectId) {
			router.push(`/projects/${projectId}`);
		},
		goToProjectTasks(projectId) {
			router.push(`/projects/${projectId}/tasks`);
		},
		goToTaskDetail(projectId, taskId, sessionId) {
			router.push(
				`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/sessions/${encodeURIComponent(sessionId)}`,
			);
		},
	};
}
