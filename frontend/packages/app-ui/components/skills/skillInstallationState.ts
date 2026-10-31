import type { PluginInstallationStatus } from "@lework/store";

export function canUpdateOrganizationSkill(status: PluginInstallationStatus | null): boolean {
	return Boolean(
		status?.installed &&
			status.marketplace_based &&
			status.update_available &&
			status.marketplace_item_id,
	);
}
