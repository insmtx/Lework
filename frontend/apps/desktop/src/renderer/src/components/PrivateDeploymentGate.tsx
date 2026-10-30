import { PrivateDeploymentGate as SharedPrivateDeploymentGate } from "@leros/app-ui";
import type { ReactNode } from "react";
import { reloadDesktopRenderer } from "../utils/reload";
import { renderInitialSparkDiscovery } from "./SparkDiscoveryList";

export function PrivateDeploymentGate({ children }: { children: ReactNode }) {
	return (
		<SharedPrivateDeploymentGate
			onReload={reloadDesktopRenderer}
			renderDiscovery={renderInitialSparkDiscovery}
		>
			{children}
		</SharedPrivateDeploymentGate>
	);
}
