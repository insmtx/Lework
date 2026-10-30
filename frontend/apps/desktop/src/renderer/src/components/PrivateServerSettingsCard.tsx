import { PrivateServerSettingsCard as SharedPrivateServerSettingsCard } from "@leros/app-ui";
import { reloadDesktopRenderer } from "../utils/reload";
import { renderSparkDiscovery } from "./SparkDiscoveryList";

export function PrivateServerSettingsCard() {
	return (
		<SharedPrivateServerSettingsCard
			onReload={reloadDesktopRenderer}
			renderDiscovery={renderSparkDiscovery}
		/>
	);
}
