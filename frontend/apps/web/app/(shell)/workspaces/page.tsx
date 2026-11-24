"use client";

import { WorkspacesView, consoleRoutes, workspacePath } from "@lework/control-ui";
import { useRouter } from "next/navigation";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function WorkspacesRoute() {
	const router = useRouter();

	return (
		<WorkspacesView
			onOpenWorkspace={(key) => router.push(workspacePath(key))}
			onOpenNodes={() => router.push(consoleRoutes.nodes)}
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
