"use client";

import { useParams, useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";
import { WorkspaceConsole } from "@/components/control-plane/workspace-console";

function WorkspaceConsoleRoute() {
	const params = useParams<{ key: string }>();
	const search = useSearchParams();
	const key = decodeURIComponent(params.key);

	return <WorkspaceConsole projectKey={key} initialSessionID={search.get("session")} />;
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
