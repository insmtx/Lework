"use client";

import type { ComposerSkillOption } from "./StructuredComposer";
import { useSkillPickerOptions } from "./useSkillPickerOptions";

/**
 * Loads the complete composer Skill list.
 */
export function useComposerSkillOptions(
	projectId: string | null | undefined,
	enabled = true,
	scope: "all" | "project" = "all",
): {
	skillOptions: ComposerSkillOption[] | undefined;
	skillsLoading: boolean;
	reloadSkillOptions: () => Promise<void>;
} {
	const { skillOptions, skillsLoading, reloadSkillOptions } = useSkillPickerOptions({
		projectId,
		includeBuiltin: true,
		scope,
		enabled,
	});

	return { skillOptions, skillsLoading, reloadSkillOptions };
}
