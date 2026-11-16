"use client";

import { ControlConsole } from "@lework/control-ui";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function ConsoleWithQuery() {
	const search = useSearchParams();
	return (
		<ControlConsole initialNodeID={search.get("node")} initialSessionID={search.get("session")} />
	);
}

export default function WorkspaceConsolePage() {
	return (
		<ControlPlaneProvider>
			<Suspense fallback={null}>
				<ConsoleWithQuery />
			</Suspense>
		</ControlPlaneProvider>
	);
}
