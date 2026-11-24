"use client";

import { PendingView, consoleSessionPath } from "@lework/control-ui";
import { useRouter } from "next/navigation";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

function PendingRoute() {
	const router = useRouter();

	return (
		<PendingView
			onOpenSession={(nodeID, sessionID) => router.push(consoleSessionPath(nodeID, sessionID))}
		/>
	);
}

export default function PendingPage() {
	return (
		<ControlPlaneProvider>
			<PendingRoute />
		</ControlPlaneProvider>
	);
}
