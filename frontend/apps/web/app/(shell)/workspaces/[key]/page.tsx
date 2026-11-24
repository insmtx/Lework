"use client";

import { WorkspaceConsole, consoleRoutes } from "@lework/control-ui";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function WorkspaceConsoleRoute() {
	const params = useParams<{ key: string }>();
	const search = useSearchParams();
	const router = useRouter();
	const key = decodeURIComponent(params.key);

	return (
		<WorkspaceConsole
			projectKey={key}
			initialSessionID={search.get("session")}
			onBack={() => router.push(consoleRoutes.workspaces)}
		/>
	);
}

export default function WorkspaceDetailPage() {
	return (
		<ControlPlaneProvider>
			<Suspense fallback={null}>
				<WorkspaceConsoleRoute />
			</Suspense>
		</ControlPlaneProvider>
	);
}
