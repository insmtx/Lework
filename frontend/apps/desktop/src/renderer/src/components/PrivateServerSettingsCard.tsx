import { PrivateServerSettingsCard as SharedPrivateServerSettingsCard } from "@leros/app-ui";
import { reloadDesktopRenderer } from "../utils/reload";

export function PrivateServerSettingsCard() {
	return <SharedPrivateServerSettingsCard onReload={reloadDesktopRenderer} />;
}
