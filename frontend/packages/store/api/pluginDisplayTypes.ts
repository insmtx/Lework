/** Shared display types for plugin/skill cards used across marketplace and org panels. */

export interface SkillMarketplaceItem {
	source_type: string;
	skill_id: string;
	name: string;
	display_name?: string;
	description: string;
	version: string;
	author: string;
	category: string;
	tags: string[] | null;
	icon: string;
	installs: number;
	verified: boolean;
	installed?: boolean;
	marketplace_available?: boolean;
	latest_version?: string;
	update_available?: boolean;
	organization_override?: boolean;
}
