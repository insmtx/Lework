"use client";

import { NodesView } from "@lework/control-ui";

import { ControlPlaneProvider } from "@/components/ControlPlaneProvider";

export default function NodesPage() {
	return (
		<ControlPlaneProvider>
			<NodesView />
		</ControlPlaneProvider>
	);
}
