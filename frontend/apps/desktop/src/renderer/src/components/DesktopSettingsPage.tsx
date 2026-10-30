import { PrivateSettingsPage } from "@leros/app-ui";
import { isPrivateDeployment } from "@leros/store";
import { Navigate } from "react-router-dom";
import { reloadDesktopRenderer } from "../utils/reload";
import { renderSparkDiscovery } from "./SparkDiscoveryList";
import { SparkManagementCard } from "./SparkManagementCard";

export function DesktopSettingsPage() {
	if (!isPrivateDeployment) {
		return <Navigate to="/workbench" replace />;
	}

	return (
		<PrivateSettingsPage
			onReload={reloadDesktopRenderer}
			renderDiscovery={renderSparkDiscovery}
			deviceManagement={<SparkManagementCard />}
		/>
	);
}
