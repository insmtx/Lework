"use client";

import { type AppNavigation, PrivateDeploymentGate, Shell } from "@lework/app-ui";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";

export function LeworkShell({ children }: { children: ReactNode }) {
	const navigation = useWebNavigation();

	return (
		<PrivateDeploymentGate>
			<Shell navigation={navigation}>{children}</Shell>
		</PrivateDeploymentGate>
	);
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
				project: "/chat",
				projectsHub: "/projects",
				taskDetail: "/chat",
				orgProfile: "/org/profile",
				orgDepartments: "/org/departments",
				orgAssistants: "/org/assistants",
				orgModels: "/org/models",
				skills: "/skills",
				automation: "/automation",
				settings: "/settings",
			}[route];
			if (!routePath) {
				router.push("/chat");
				return;
			}
			router.push(routePath);
		},
		goToPath(path) {
			router.push(path);
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
		goToAutomationDetail(publicId) {
			router.push(`/automation/${encodeURIComponent(publicId)}`);
		},
	};
}
