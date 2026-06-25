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
				chat: "/chat",
				workbench: "/workbench",
				tasks: "/tasks",
				project: "/workbench",
				projectsHub: "/projects",
				taskDetail: "/workbench",
				digitalAssistant: "/assistants",
				aiTeammates: "/ai-teammates",
				knowledge: "/knowledge",
				skills: "/skills",
				settings: "/settings",
			}[route];
			router.push(routePath);
		},
		goToProject(projectId) {
			router.push(`/projects/${projectId}`);
		},
		goToTaskDetail(projectId, taskId, sessionId) {
			const search = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : "";
			router.push(`/projects/${projectId}/tasks/${taskId}${search}`);
		},
	};
}
