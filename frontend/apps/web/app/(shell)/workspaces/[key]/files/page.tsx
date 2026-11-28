"use client";

import { ProjectFilesView, consoleRoutes } from "@lework/control-ui";
import { useParams, useRouter } from "next/navigation";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function ProjectFilesRoute() {
	const params = useParams<{ key: string }>();
	const router = useRouter();
	const key = decodeURIComponent(params.key);

	return (
		<ProjectFilesView projectKey={key} onBack={() => router.push(consoleRoutes.workspaces)} />
	);
}

export default function ProjectFilesPage() {
	return (
		<ControlPlaneProvider>
			<ProjectFilesRoute />
		</ControlPlaneProvider>
	);
}
