import type { PluginComposerOption } from "@leros/store";

export type SkillSourceLabel = "系统" | "市场" | "组织";

/** Returns the user-facing ownership label for a Skill candidate. */
export function getSkillSourceLabel(skill: PluginComposerOption): SkillSourceLabel {
	if (skill.source === "builtin" || skill.origin === "builtin_worker") return "系统";
	if (skill.source === "marketplace" || skill.origin === "marketplace") return "市场";
	return "组织";
}
