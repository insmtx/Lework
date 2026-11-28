"use client";

import {
	consoleRoutes,
	WorkspacesView,
	workspaceFilesPath,
	workspacePath,
} from "@lework/control-ui";
import { useRouter } from "next/navigation";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function WorkspacesRoute() {
	const router = useRouter();

	return (
		<WorkspacesView
			onOpenWorkspace={(key) => router.push(workspacePath(key))}
			onOpenNodes={() => router.push(consoleRoutes.nodes)}
			onBrowseWorkspace={(key) => router.push(workspaceFilesPath(key))}
		/>
	);
}

export default function WorkspacesPage() {
	return (
		<ControlPlaneProvider>
			<WorkspacesRoute />
		</ControlPlaneProvider>
	);
}
