import { PrivateSettingsPage } from "@leros/app-ui";
import { isPrivateDeployment } from "@leros/store";
import { Navigate } from "react-router-dom";
import { reloadDesktopRenderer } from "../utils/reload";

export function DesktopSettingsPage() {
	if (!isPrivateDeployment) {
		return <Navigate to="/workbench" replace />;
	}

	return <PrivateSettingsPage onReload={reloadDesktopRenderer} />;
}
