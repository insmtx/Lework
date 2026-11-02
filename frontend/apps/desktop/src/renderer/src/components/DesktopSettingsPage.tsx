import { PrivateSettingsPage } from "@lework/app-ui";
import { isPrivateDeployment } from "@lework/store";
import { Navigate } from "react-router-dom";
import { reloadDesktopRenderer } from "../utils/reload";

export function DesktopSettingsPage() {
	if (!isPrivateDeployment) {
		return <Navigate to="/workbench" replace />;
	}

	return <PrivateSettingsPage onReload={reloadDesktopRenderer} />;
}
