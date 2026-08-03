"use client";

import type { ComposerSkillOption } from "./StructuredComposer";
import { useSkillPickerOptions } from "./useSkillPickerOptions";

/**
 * Loads the complete composer Skill list.
 */
export function useComposerSkillOptions(
	projectId: string | null | undefined,
	enabled = true,
): {
	skillOptions: ComposerSkillOption[] | undefined;
	skillsLoading: boolean;
} {
	const { skillOptions, skillsLoading } = useSkillPickerOptions({
		projectId,
		includeBuiltin: true,
		enabled,
	});

	return { skillOptions, skillsLoading };
}
